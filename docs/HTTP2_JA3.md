# HTTP/2 与 JA3 评估结论

> 迁移清单第 6 步。结论：**本阶段不实现 JA3 / local_fanout**，继续使用 `fasthttp` HTTP/1.1 + 代理池。

## 上游能力（biliTickerBuy）

- `util/h2client/*`：HTTP/2 客户端与连接复用
- JA3 / TLS 指纹伪装（浏览器指纹对齐）
- `local_fanout`：按源 IP/代理维持多条 createV2 连接并预热

## Go 生态可选方案

| 方案 | 说明 | 风险/成本 |
|------|------|-----------|
| 标准库 `net/http` + HTTP/2 | 易用，无 JA3 | 指纹与 Chrome 差异大 |
| `fasthttp`（当前） | 高性能 HTTP/1.1 | 无原生 HTTP/2/JA3 |
| `refraction-networking/utls` + 自定义 transport | 可模拟 Chrome JA3 | 维护成本高，需自研连接池/代理拨号 |
| `bogdanfinn/tls-client` 等 | 封装 utls | 依赖体积大，API 与 fasthttp 不兼容 |
| CGO / 外部浏览器 | 最接近真浏览器 | 集群 worker 不适合 |

## 决策（更新）

1. **HTTP 层**：已从 `fasthttp` 迁到标准库 `net/http` + **uTLS `HelloChrome_Auto`**，TLS ClientHello 模拟桌面 Chrome，避免 Go 默认 JA3。
2. **HTTP 头**：`BrowserFingerprint` 对齐上游 `build_headers_from_browser_state`（UA / sec-ch-ua / sec-fetch-* / Accept-Language 等），会话内固定。
3. **多连接（HTTP/1.1）**：`CONN_PER_HOST` 控制 `MaxConnsPerHost` / 空闲池；`CREATE_BATCH_SIZE` 并发 createV2。
4. **预热**：`ENABLE_WARMUP` 时，开售前与 `100001` 后执行 `Warmup`（并发 GET 首页/详情 + 详情复检）。
5. **仍不实现** 完整 HTTP/2 `local_fanout`（ALPN 固定 `http/1.1` + uTLS Chrome）。
6. **代理**：HTTP CONNECT / SOCKS5 在 Dial 层完成后再做 uTLS 握手。

## 与迁移清单对齐

- [x] 调研方案
- [x] 决定：本阶段不迁移 local_fanout/JA3
- [x] 文档明确 fasthttp 行为差异
- [ ] 实现 H2/JA3（搁置，非阻塞主链路）
