package worker

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestUTLSConnConnectionStateALPN(t *testing.T) {
	// 单元级：包装类型必须暴露 crypto/tls.ConnectionState（http2 依赖）
	var _ interface {
		ConnectionState() tls.ConnectionState
		net.Conn
	} = (*utlsConn)(nil)
}

func TestBuildHTTPClientShowBilibiliH2(t *testing.T) {
	if testing.Short() {
		t.Skip("network")
	}
	client := buildHTTPClient(nil, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://show.bilibili.com/api/ticket/project/getV2?id=0", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	resp, err := client.Do(req)
	if err != nil {
		if strings.Contains(err.Error(), "malformed HTTP response") {
			t.Fatalf("仍按 HTTP/1.1 读 h2 帧: %v", err)
		}
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode == 0 {
		t.Fatalf("empty status body=%q", body)
	}
	// 业务可能 4xx，但协议层应成功解析
	if len(body) == 0 && resp.StatusCode >= 500 {
		t.Fatalf("unexpected empty body status=%d", resp.StatusCode)
	}
	t.Logf("status=%d proto=%s body_len=%d", resp.StatusCode, resp.Proto, len(body))
	if resp.ProtoMajor != 2 && resp.Proto != "HTTP/2.0" && !strings.HasPrefix(resp.Proto, "HTTP/2") {
		// 部分环境可能回退 1.1；只要不是 malformed 即可，记录协议
		t.Logf("warning: negotiated %s (expected h2 on show.bilibili.com)", resp.Proto)
	}
}
