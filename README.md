# task265-atomjump 原子钟频率跳变来源判别服务

高精度计时实验室的原子钟频率跳变来源判别后端服务。接收频率样本、环境读数与多链路比对数据，
统一时间基准后检测频率跳变段，依据环境/链路/设备三源相干性评分来源候选，
最终隔离不可信链路、锁定参考钟并发布不可变诊断快照。

## 业务闭环

1. 接入：时钟上报频率样本（幂等序号 + 单位校验）、环境读数、比对链路。
2. 检测：60 秒窗口切分 → 窗口状态（稳定/跳变/缺口/排除）→ 连续 jump 合并为跳变段。
3. 判别：来源评分（环境相干 / 链路相干 / 设备自身）→ 人工确认。
4. 封存：隔离链路、锁定参考钟、发布不可变快照，时钟封存。

## 状态机

- 时钟：`collecting → analyzing → review → confirmed → sealed`（封存后不可改）
- 窗口：`raw → stable / jump / gap / excluded`
- 链路：`healthy → suspect / isolated`
- 快照：`draft → published → superseded`（published 不可变）

## 标准命令

```bash
# 构建
CGO_ENABLED=0 GOTOOLCHAIN=local go build ./...
# 静态检查
CGO_ENABLED=0 GOTOOLCHAIN=local go vet ./...
# 单元测试
CGO_ENABLED=0 GOTOOLCHAIN=local go test ./...
# 端到端自检（含重启恢复验证）
CGO_ENABLED=0 GOTOOLCHAIN=local go run ./cmd/task265-atomjump --smoke-test --db smoke.db
# 启动服务
CGO_ENABLED=0 GOTOOLCHAIN=local go run ./cmd/task265-atomjump --addr :8080 --db atomjump.db
```

## 技术栈

- Go 1.26.3（GOTOOLCHAIN=local，CGO_ENABLED=0）
- SQLite（modernc.org/sqlite 纯 Go 驱动，无需 CGO），版本锁定见 `component-versions.json`

## API 入口

全部以 `/api` 为前缀（26 个），覆盖时钟、样本、环境、链路、窗口/跳变/判别、快照、统计与自检。
完整清单见 `BENZHI_README.md`。
