# AGENTS.md

BiliTickerStorm：B 站抢票调度集群（Go master/worker）。配置 JSON 由 [biliTickerBuy](https://github.com/mikumifa/biliTickerBuy) 生成，放入 `data/`。

## 架构

| 组件 | 入口 | 端口 | 说明 |
|------|------|------|------|
| ticket-master | `cmd/master` | gRPC `:40052` + WebUI HTTP `:8080` | 单实例；调度 + 内嵌控制台（`internal/master/web`） |
| ticket-worker | `cmd/worker` | gRPC `:40051` | 可多实例；向 master 注册/心跳，执行抢票 |

- 业务：`internal/master`、`internal/worker`、`internal/common`（含 `workercfg`）
- gRPC：`proto/*.proto` → 生成物已提交于 `internal/*/pb/`（**勿手改**）
- 镜像：`master.Dockerfile` / `worker.Dockerfile`；compose 根目录 `docker-compose.yml`；K8s `helm/`（worker 为 DaemonSet）
- 发布：仅 `v*` tag → `.github/workflows/main.yml` → Docker Hub `mikumifa/bili-ticker-storm-*` + Helm chart 到 `gh-pages`
- Worker HTTP：`net/http` + uTLS Chrome 指纹（非 fasthttp）；完整 H2 fanout 未做，见 `docs/HTTP2_JA3.md`
- 无 gt-python / 极验服务

## 命令

```bash
# 模块路径 biliTickerStorm，Go 1.24
go build -o master ./cmd/master
go build -o worker ./cmd/worker
go test ./internal/worker/...
go test ./internal/common/...
go test ./internal/common/workercfg/...

# 改 proto 后（仓库根目录；生成物要提交）
protoc --go_out=. --go-grpc_out=. proto/master.proto
protoc --go_out=. --go-grpc_out=. proto/worker.proto

docker compose up --build
CONFIG_PATH=data go run ./cmd/master   # WebUI http://127.0.0.1:8080
```

**勿**常规回归跑 `go test ./...`：`internal/common.TestSleepUntilAccurate` 会真实等待约 **300s** + NTP。

## 配置（易错）

**部署级 env（WebUI 不可改）**

| 进程 | 变量 | 说明 |
|------|------|------|
| master | `CONFIG_PATH` | **必需**；任务 JSON + `worker_settings.json` + `accounts/` |
| master | `WEB_ADDR` / `WEB_TOKEN` | 默认 `:8080`；token 空则 API 不鉴权 |
| worker | `MASTER_SERVER_ADDR` | **必需**，如 `ticket-master:40052` |

**Worker 运行参数（env 默认 + WebUI 覆盖）**

- 文件：`{CONFIG_PATH}/worker_settings.json`；WebUI「高级设置」；心跳 `RegisterReply` 下发
- 合并：非空 settings **覆盖** env；`MASTER_SERVER_ADDR` 永不被覆盖
- Worker 读配置用 `GetConfig()`（热更新会换指针）；勿假设启动时的 `Cfg` 永不改
- 常用：`TICKET_INTERVAL`（**毫秒**）、`RATE_LIMIT_DELAY_MS`、`RISK_*`、`PROXY_*`、`CONN_PER_HOST`、`CREATE_BATCH_SIZE`、`ENABLE_WARMUP`、通知 `PUSHPLUS_*`/`BARK_*`/`SERVERCHAN_*`/`TELEGRAM_*`、`TICKET_TIME_START`（`2006-01-02T15:04` Asia/Shanghai；未设可读任务 `sale_start`）

详情：`docs/WebUI.md`、`docs/风控对齐.md`。

## 约定

- 勿提交真实 cookies / 抢票 JSON；`data/` 仅本地
- 改 gRPC：先 `proto/` → `protoc` → 提交更新后的 `pb/*.go`
- Helm：`ticketWorker.time` 模板未用，改定时用 `ticketTimeStart`
