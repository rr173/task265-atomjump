// 原子钟频率跳变来源判别服务入口。
//
// 用法：
//   task265-atomjump --addr :8080 --db atomjump.db
//   task265-atomjump --smoke-test --db smoke.db
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"task265-atomjump/internal/httpapi"
	"task265-atomjump/internal/model"
	"task265-atomjump/internal/sampling"
	"task265-atomjump/internal/service"
	"task265-atomjump/internal/snapshot"
	"task265-atomjump/internal/store"
)

func main() {
	addr := flag.String("addr", ":8080", "HTTP listen address")
	dbPath := flag.String("db", "atomjump.db", "SQLite database path")
	smoke := flag.Bool("smoke-test", false, "run end-to-end self test and exit")
	flag.Parse()

	if *smoke {
		if err := runSmokeTest(*dbPath); err != nil {
			log.Fatalf("smoke test failed: %v", err)
		}
		fmt.Println("smoke test passed")
		return
	}

	st, err := store.Open(*dbPath)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer st.Close()

	svc := service.New(st)
	if err := svc.Recover(context.Background()); err != nil {
		log.Fatalf("recover: %v", err)
	}
	srv := &http.Server{
		Addr:              *addr,
		Handler:           httpapi.New(svc).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("task265-atomjump listening on %s (db=%s)", *addr, *dbPath)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("listen: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}

// runSmokeTest 端到端自检：
//  1. 创建参考钟与铯钟，上报样本与环境读数，建立比对链路；
//  2. 触发分析（窗口化+跳变检测+来源判别），确认来源候选；
//  3. 隔离链路、创建并发布诊断快照；
//  4. 关闭数据库并重开同一文件，验证全部状态持久化与重启恢复；
//  5. 校验快照不可变与封存拒绝修改等错误边界。
func runSmokeTest(dbPath string) error {
	_ = os.Remove(dbPath)
	st, err := store.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}
	svc := service.New(st)

	// 1) 参考钟（UTC 主基准）与铯钟
	ref, err := st.Clocks.Create("utc-reference", "reference", 10000000.0, true, 0)
	if err != nil {
		return fmt.Errorf("create ref: %w", err)
	}
	if _, err := st.Clocks.MarkIsReference(ref.ID); err != nil {
		return err
	}
	clk, err := st.Clocks.Create("cs-1", "cesium", 9192631770.0, false, ref.ID)
	if err != nil {
		return fmt.Errorf("create clock: %w", err)
	}

	// 2) 上报样本：前 8 个稳定（偏差 ~0.1ppb），后 4 个跳变（偏差 ~+2.0ppb）
	base := time.Now().UTC().Truncate(time.Minute)
	for i := 0; i < 12; i++ {
		seq := int64(i + 1)
		taken := base.Add(time.Duration(i) * 15 * time.Second)
		var freq float64
		if i < 8 {
			freq = clk.NominalHz * (1 + 0.1e-9)
		} else {
			freq = clk.NominalHz * (1 + 2.0e-9)
		}
		if _, err := svc.Receive.Ingest(sampling.IngestInput{
			ClockID: clk.ID, Seq: seq, TakenAt: taken, FreqHz: freq, Baseline: "",
		}); err != nil {
			return fmt.Errorf("ingest sample %d: %w", i, err)
		}
	}
	// 环境读数：温度稳定（无大波动）
	for i := 0; i < 4; i++ {
		if _, err := svc.Receive.IngestEnv(&model.EnvReading{
			ClockID: clk.ID, TakenAt: base.Add(time.Duration(i*45) * time.Second),
			TempC: 25.0, HumidityPct: 40.0, PressureHPa: 1013.0, Vibration: 0.05,
		}); err != nil {
			return err
		}
	}
	// 比对链路：两条健康
	if _, err := st.Links.Upsert(clk.ID, ref.ID, 0.1, 5); err != nil {
		return err
	}

	// 3) 分析
	res, err := svc.AnalyzeClock(clk.ID)
	if err != nil {
		return fmt.Errorf("analyze: %w", err)
	}
	if len(res.Jumps) == 0 {
		return fmt.Errorf("expected at least 1 jump, got 0")
	}
	jump := res.Jumps[0]

	// 4) 来源判别（环境稳定 -> 不应判 environment 最高）
	attr, err := svc.AttributeJump(jump.ID)
	if err != nil {
		return err
	}
	if attr.Verdict == "environment" {
		return fmt.Errorf("unexpected environment verdict with stable temp")
	}
	// 人工确认设备来源
	if err := svc.ConfirmJump(jump.ID, "device"); err != nil {
		return err
	}

	// 5) 隔离链路 + 发布快照
	links, err := st.Links.ListBySubject(clk.ID)
	if err != nil {
		return err
	}
	if len(links) == 0 {
		return fmt.Errorf("expected links")
	}
	if _, err := svc.LinkAction(links[0].ID, true); err != nil {
		return err
	}
	draft, err := svc.Publish.CreateDraft(snapshot.DraftInput{
		ClockID: clk.ID, ReferenceID: ref.ID,
		Summary: "cesium jump attributed to device state switch",
	})
	if err != nil {
		return err
	}
	published, err := svc.Publish.Publish(draft.ID)
	if err != nil {
		return err
	}
	if published.Status != "published" {
		return fmt.Errorf("expected published status, got %s", published.Status)
	}
	if len(published.IsolatedLinks) == 0 {
		return fmt.Errorf("expected isolated links captured in snapshot")
	}

	// 6) 错误边界：封存后拒绝样本
	if _, err := svc.Receive.Ingest(sampling.IngestInput{
		ClockID: clk.ID, Seq: 99, TakenAt: base.Add(10 * time.Minute), FreqHz: clk.NominalHz, Baseline: "",
	}); err == nil {
		return fmt.Errorf("expected sealed clock reject sample")
	}

	// 7) 关闭并重开：验证重启恢复
	closedClockID, closedSnapID := clk.ID, published.ID
	closedSummary := published.Summary
	if err := st.Close(); err != nil {
		return err
	}
	st2, err := store.Open(dbPath)
	if err != nil {
		return fmt.Errorf("reopen: %w", err)
	}
	defer st2.Close()
	got, err := st2.Clocks.Get(closedClockID)
	if err != nil {
		return fmt.Errorf("reload clock: %w", err)
	}
	if got.Status != "sealed" {
		return fmt.Errorf("expected sealed after publish, got %s", got.Status)
	}
	snaps, err := st2.Snapshots.List()
	if err != nil {
		return err
	}
	if len(snaps) == 0 || snaps[0].ID != closedSnapID || snaps[0].Summary != closedSummary {
		return fmt.Errorf("snapshot not recovered: %+v", snaps)
	}
	wins, err := st2.Windows.ListByClock(closedClockID)
	if err != nil {
		return err
	}
	if len(wins) == 0 {
		return fmt.Errorf("windows not recovered")
	}
	fmt.Printf("smoke: clock=%d samples=12 windows=%d jumps=%d snapshot=%d status=%s\n",
		closedClockID, len(wins), len(res.Jumps), len(snaps), got.Status)
	return nil
}
