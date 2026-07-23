# AGENTS.md

BiliTickerStorm：B 站抢票调度集群（Go master/worker + Python 验证码服务）。配置 JSON 由 [biliTickerBuy](https://github.com/mikumifa/biliTickerBuy) 生成，放入 `data/`。

## 架构

| 组件 | 入口 | 端口 | 说明 |
|------|------|------|------|
| ticket-master | `cmd/master` | gRPC `:40052` | 单实例；从 `CONFIG_PATH` 加载 `*.json` 任务并调度 |
| ticket-worker | `cmd/worker` | gRPC `:40051` | 可多实例；向 master 注册/心跳，执行抢票 |
| gt-python | `python/main.py` | HTTP `:8000` | FastAPI；`POST /validate/geetest` |

- 业务代码：`internal/master`、`internal/worker`、`internal/common`
- gRPC 定义：`proto/*.proto` → 生成物已提交于 `internal/*/pb/`（勿手改）
- 部署：根目录 Dockerfile + `docker-compose.yml`；K8s 用 `helm/`（worker 为 DaemonSet）
- 镜像推送：仅 `v*` tag 触发 `.github/workflows/main.yml` → Docker Hub `mikumifa/bili-ticker-storm-*` + Helm 到 `gh-pages`

## 常用命令

```bash
# Go（模块路径 biliTickerStorm，Go 1.24）
go build -o master ./cmd/master
go build -o worker ./cmd/worker
go test ./internal/common/...
go test ./internal/worker/...

# 修改 proto 后（仓库根目录）
protoc --go_out=. --go-grpc_out=. proto/master.proto
protoc --go_out=. --go-grpc_out=. proto/worker.proto

# Python（目录 python/，用 uv + uv.lock）
cd python && uv sync --locked
cd python && fastapi dev --host 0.0.0.0   # 默认 :8000

# 本地全栈
docker compose up --build
```

本地跑 master 至少：`CONFIG_PATH=data`（见 `.idea/runConfigurations/master.xml`）。

## 环境变量（必填/易错）

**master**
- `CONFIG_PATH`：任务 JSON 目录（必需）

**worker**
- `MASTER_SERVER_ADDR`：如 `ticket-master:40052`（必需）
- `GT_BASE_URL`：如 `http://gt-python:8000`（必需）
- `TICKET_INTERVAL`：默认 `300`，**代码按毫秒**用于 `time.Sleep`（`buy.go`）；`config.go` 日志写「秒」不准确
- `TICKET_TIME_START`：可选，格式 `2006-01-02T15:04`，时区 **Asia/Shanghai**
- `PUSHPLUS_TOKEN`：可选

**gt-python**
- `GEETEST_WORKER_COUNT`：并行验证码进程数，默认 `10`

## 测试注意

- `internal/common.TestSleepUntilAccurate` 会真实等待约 **300s** + NTP，勿在常规回归中无脑 `go test ./...` 全跑
- `internal/worker/captcha_test.go` 依赖外网 B 站接口、本地 gt-python，且需 `data.json` 配置文件

## 约定

- 不要提交真实 cookies / 抢票 JSON 到仓库；`data/` 仅放本地配置
- 改 gRPC 接口：先改 `proto/`，再 `protoc`，提交更新后的 `pb/*.go`
- Python 依赖以 `python/uv.lock` 为准；镜像构建走 `python.Dockerfile`（含 rust/cargo，因 `bili-ticket-gt-python`）
- Helm `values.yaml` 里 `ticketWorker.time` 字段未被模板使用，改时间用 `ticketTimeStart`
