# WebUI 使用说明

Master 内嵌控制台，默认监听 **`WEB_ADDR=:8080`**，与 gRPC `:40052` 同进程。

## 一键打开

### 本地

```powershell
cd E:\vuedocs\biliTickerStorm
$env:CONFIG_PATH = "E:\vuedocs\biliTickerStorm\data"
# 可选：$env:WEB_TOKEN = "change-me"
go run ./cmd/master
```

浏览器打开：<http://127.0.0.1:8080>

### Docker Compose

```bash
docker compose up --build -d
```

浏览器打开：<http://localhost:8080>

可选：在项目根目录创建 `.env`（已被 gitignore），compose 会加载：

```env
WEB_TOKEN=your-secret
PUSHPLUS_TOKEN=
TICKET_INTERVAL=300
# TICKET_TIME_START=2026-07-24T13:14
# RATE_LIMIT_DELAY_MS=300
# RISK_LOCAL_RETRIES=5
# PROXY_LIST=
```

### Helm

```bash
helm install bili-ticker-storm ./helm \
  --set ticketMaster.hostDataPath=/your/host/data/path \
  --set ticketMaster.webToken="your-secret" \
  --set ticketWorker.pushplusToken="" \
  --set ticketWorker.ticketInterval="300"
```

端口转发查看 UI：

```bash
kubectl port-forward svc/ticket-master 8080:8080
```

然后打开 <http://127.0.0.1:8080>。

## 鉴权

| 场景 | 行为 |
|------|------|
| 未设置 `WEB_TOKEN` | 跳过鉴权页，直接进入控制台 |
| 已设置 `WEB_TOKEN` | 先显示独立鉴权页，校验通过后进入主页；Token 存 `localStorage.bts_token` |
| API | `Authorization: Bearer <token>` 或 `?token=` |

`GET /api/v1/health` 始终无需鉴权。主页顶栏「退出」可清除 Token 回到鉴权页。

## UI（MDUI · Material Design 3）

- 技术：原生 ES Module + [MDUI 2](https://www.mdui.org/)（CDN：`unpkg.com/mdui@2`，**零前端构建、无本地 UI 库 vendor**）
- 布局：`mdui-layout` + Top app bar + Navigation rail（窄屏 Drawer / Tabs）；默认深色，可切换浅色（`localStorage.bts_theme` + `mdui.setTheme`）
- 反馈：`mdui.snackbar` / `mdui.confirm` / `mdui.dialog`、顶栏加载条
- 业务静态资源：`//go:embed all:static` 打入 master；需浏览器能访问 unpkg（或自行改 CDN）

## 页面导航

| 页面 | 作用 |
|------|------|
| 集群看板 | Worker / Task 状态（约 3s 刷新） |
| 账号登录 | 扫码登录，账号存 `CONFIG_PATH/accounts/` |
| 生成配置 | 项目/票档/购票人 → JSON 落盘并入队 |
| 操作任务 | 上传 JSON、重入队、删除、目录重载 |
| 高级设置 | **Worker 集群设置**（通知/代理/风控/连接，写入 `worker_settings.json` 并心跳下发）、运行时参数 |
| 版本更新 | 版本号与 GitHub Releases 链接 |
| 事件日志 | Master 调度/风控事件（可过滤/清空） |
| 说明 | 使用步骤 |

## Worker 集群设置

路径：`{CONFIG_PATH}/worker_settings.json`。WebUI「高级设置」可编辑；master 在 worker 心跳 `RegisterReply` 中下发。

- 非空字段覆盖 worker 环境变量；`MASTER_SERVER_ADDR` 不可配置
- 密钥字段 GET 脱敏；保存时若仍为脱敏值则保留原值
- API：`GET/PUT /api/v1/settings/worker`，`GET /api/v1/settings/worker/export`（完整 .env 片段）
- **`worker_settings.json` 不会被当作抢票任务**：启动扫描、目录重载、配置列表均跳过该保留文件名；也不能用该名称创建任务

## 主要 API

- `GET /api/v1/meta` — 版本、超时、功能开关、鉴权是否开启  
- `GET/PUT /api/v1/settings/worker` — worker 集群运行参数  
- `GET /api/v1/overview` · `/workers` · `/tasks` · `/events`  
- `POST /api/v1/tasks` — JSON 或 multipart 上传  
- `POST /api/v1/tasks/{id}/requeue` · `DELETE /api/v1/tasks/{id}`  
- `POST /api/v1/tasks/reload` — body `{"names":["a"]}` 入队所选；`{"all":true}` 整目录未入队文件  

- `POST /api/v1/auth/qr/start` · `GET /api/v1/auth/qr/poll`  
- `POST /api/v1/configs/generate` — 默认只写盘；`start_task: true` 时才入队  


配置预览接口会对 cookies / 证件号等字段脱敏。

## 配置格式（与 biliTickerBuy 互通）

WebUI「生成配置」产出的 JSON 与 Buy「生成配置」字段对齐，可直接互相拷贝：

| 字段 | 说明 |
|------|------|
| `username` / `detail` / `count` / `screen_id` / `project_id` / `sku_id` | 核心 |
| `pay_money` | 票价(分) × count |
| `buyer_info` / `buyer` / `tel` / `deliver_info` / `cookies` / `phone` | 实名与配送 |
| `is_hot_project` / `link_id?` | 与 Buy 一致 |
| **`sale_start`** | 票档起售时间；**不用于 create 请求** |

**`sale_start` 用途（对齐 Buy）**

- Buy：操作抢票页根据配置 `sale_start` **自动填写**抢票开始时间。  
- Storm Worker 定时优先级：
  1. 环境变量 `TICKET_TIME_START`（全局，格式 `2006-01-02T15:04` Asia/Shanghai）
  2. 否则任务 JSON 的 `sale_start`（按任务；支持 `2006-01-02 15:04:05` / ISO / unix）
  3. 都没有或已过起售时刻 → 立即开抢

落盘格式：`UTF-8`、`indent=4`、中文不转义；文件名过滤 `\ / : * ? " < > |`，**保留中文**。

## 安全注意

- 不要把 `data/*.json`、`data/accounts/`、真实 `WEB_TOKEN` 提交到 git。  
- 生产环境务必设置 `WEB_TOKEN`，并限制 8080 的网络暴露。  
