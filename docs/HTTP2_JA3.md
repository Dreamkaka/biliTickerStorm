# HTTP/2 与 JA3

## 结论（现行）

Worker 使用 **uTLS `HelloChrome_Auto` + HTTP/2（优先）+ HTTP/1.1 回退**：

1. TLS ClientHello 模拟桌面 Chrome（JA3 接近浏览器）。
2. ALPN 声明 `h2, http/1.1`；`show.bilibili.com` 等会协商 **h2**。
3. `alpnTransport`：HTTPS 先走 `golang.org/x/net/http2`；仅当 ALPN 非 h2 时回退 `net/http` HTTP/1.1。
4. 自定义 `DialTLS` 返回 `utlsConn`，实现 `crypto/tls.ConnectionState`，供 http2 读协商协议。
5. 代理：HTTP CONNECT / SOCKS5 在 Dial 层完成后再 uTLS 握手。
6. **仍不实现** 上游完整 `local_fanout`（多源 IP 多 H2 连接扇出）；多连接靠 `CONN_PER_HOST` + `CREATE_BATCH_SIZE` + `Warmup`。

## 故障背景

若 ALPN 协商到 h2 却用 HTTP/1.1 解析，会出现：

```text
net/http: HTTP/1.x transport connection broken: malformed HTTP response "\x00\x00\x12\x04..."
```

（`\x00\x00\x12\x04` 为 HTTP/2 SETTINGS 帧前缀。）

## 上游对照（biliTickerBuy）

| 能力 | 上游 | 本仓库 |
|------|------|--------|
| HTTP/2 | `util/h2client/*` | `x/net/http2` + uTLS |
| JA3 / Chrome TLS | 浏览器态 | `HelloChrome_Auto` |
| local_fanout | 多连接预热扇出 | 未做；`CONN_PER_HOST`/`CREATE_BATCH_SIZE`/`ENABLE_WARMUP` |

## 相关代码

- `internal/worker/tls_dial.go`：`chromeTLSDialer`、`utlsConn`、`alpnTransport`、`buildHTTPClient`
- `internal/worker/fingerprint.go`：HTTP 头指纹
- `internal/worker/warmup.go`：开售前/100001 后预热
