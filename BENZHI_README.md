# BENZHI 评测说明

基于 Go 实现的原子钟频率跳变来源判别后端服务，一款后端服务，完成频率样本窗口化、跳变段检测、环境/链路/设备三源相干性评分与不可变诊断快照发布。

## 启动

```bash
CGO_ENABLED=0 GOTOOLCHAIN=local go run ./cmd/task265-atomjump --addr :8080 --db atomjump.db
```

## 自检（不启动长驻服务）

```bash
go run ./cmd/task265-atomjump --smoke-test --db /tmp/atomjump-smoke.db
```

`--smoke-test` 会真实创建参考钟与铯钟、上报样本与环境读数、触发窗口化/跳变检测/来源判别、发布诊断快照，关闭并重开数据库验证持久化与重启恢复，最后以 0 退出码结束。

## 构建门禁

```bash
CGO_ENABLED=0 GOTOOLCHAIN=local go build ./...
CGO_ENABLED=0 GOTOOLCHAIN=local go vet   ./...
CGO_ENABLED=0 GOTOOLCHAIN=local go test  ./...
go run ./cmd/task265-atomjump --smoke-test --db /tmp/atomjump-smoke.db
```

## HTTP API（前缀 /api）

- 时钟：POST/GET /api/clocks、GET /api/clocks/{id}、PATCH /api/clocks/{id}/status、POST /api/clocks/{id}/seal
- 样本：POST/GET /api/clocks/{id}/samples
- 环境：POST/GET /api/clocks/{id}/env
- 链路：POST/GET /api/clocks/{id}/links、POST /api/links/{id}/isolate、POST /api/links/{id}/restore
- 分析：POST /api/clocks/{id}/analyze、GET /api/clocks/{id}/windows、GET /api/clocks/{id}/jumps
- 判别：GET /api/jumps/{id}、GET /api/jumps/{id}/candidates、POST /api/jumps/{id}/confirm
- 快照：POST /api/snapshots、POST /api/snapshots/{id}/publish、GET /api/snapshots、GET /api/snapshots/{id}
- 其他：GET /api/health、GET /api/stats、GET /api/selfcheck

## 持久化

SQLite（modernc.org/sqlite，CGO 无关）。建表：clocks、samples、env_readings、compare_links、freq_windows、jump_segments、source_candidates、snapshots。样本 `(clock_id,seq)` 幂等，窗口 `(clock_id,start_at)` 幂等，重启同一数据库可恢复活动窗口与快照。
