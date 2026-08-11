package worker

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	utls "github.com/refraction-networking/utls"
	"golang.org/x/net/proxy"
)

// chromeTLSDialer 使用 uTLS 模拟 Chrome ClientHello，避免 Go 默认 TLS 指纹。
type chromeTLSDialer struct {
	timeout   time.Duration
	proxyURL  *url.URL // nil = 直连
	proxyKind string   // http / socks5
}

func newChromeTLSDialer(proxyEntry *ProxyEntry) *chromeTLSDialer {
	d := &chromeTLSDialer{timeout: 15 * time.Second}
	if proxyEntry != nil && proxyEntry.URL != nil {
		d.proxyURL = proxyEntry.URL
		d.proxyKind = proxyEntry.Kind
	}
	return d
}

func (d *chromeTLSDialer) DialTLSContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	raw, err := d.dialTCP(ctx, addr)
	if err != nil {
		return nil, err
	}
	cfg := &utls.Config{
		ServerName:         host,
		InsecureSkipVerify: false,
		MinVersion:         tls.VersionTLS12,
		// 当前 Transport 按 HTTP/1.1 解析；ALPN 仅声明 http/1.1，避免协商到 h2 后半截帧无法解析
		NextProtos: []string{"http/1.1"},
	}
	// HelloChrome_Auto 跟随较新 Chrome ClientHello（JA3 接近浏览器）
	uconn := utls.UClient(raw, cfg, utls.HelloChrome_Auto)
	// 覆盖 spec 中的 ALPN，与 NextProtos / 上层 HTTP/1.1 客户端一致
	spec, err := utls.UTLSIdToSpec(utls.HelloChrome_Auto)
	if err == nil {
		for i := range spec.Extensions {
			if alpn, ok := spec.Extensions[i].(*utls.ALPNExtension); ok {
				alpn.AlpnProtocols = []string{"http/1.1"}
			}
		}
		if err := uconn.ApplyPreset(&spec); err != nil {
			log.Warnf("utls ApplyPreset: %v，继续默认握手", err)
		}
	}
	if err := uconn.HandshakeContext(ctx); err != nil {
		_ = raw.Close()
		return nil, fmt.Errorf("utls handshake %s: %w", host, err)
	}
	return uconn, nil
}

func (d *chromeTLSDialer) dialTCP(ctx context.Context, addr string) (net.Conn, error) {
	if d.proxyURL == nil {
		var nd net.Dialer
		nd.Timeout = d.timeout
		return nd.DialContext(ctx, "tcp", addr)
	}
	switch d.proxyKind {
	case "socks5":
		return d.dialSOCKS5(ctx, addr)
	default:
		return d.dialHTTPProxy(ctx, addr)
	}
}

func (d *chromeTLSDialer) dialHTTPProxy(ctx context.Context, addr string) (net.Conn, error) {
	proxyAddr := d.proxyURL.Host
	var nd net.Dialer
	nd.Timeout = d.timeout
	conn, err := nd.DialContext(ctx, "tcp", proxyAddr)
	if err != nil {
		return nil, fmt.Errorf("dial http proxy %s: %w", proxyAddr, err)
	}
	req := &http.Request{
		Method: http.MethodConnect,
		URL:    &url.URL{Opaque: addr},
		Host:   addr,
		Header: make(http.Header),
		// 避免暴露 Go 默认 UA
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")
	req.Header.Set("Proxy-Connection", "Keep-Alive")
	if d.proxyURL.User != nil {
		user := d.proxyURL.User.Username()
		pass, _ := d.proxyURL.User.Password()
		auth := base64.StdEncoding.EncodeToString([]byte(user + ":" + pass))
		req.Header.Set("Proxy-Authorization", "Basic "+auth)
	}
	if err := req.Write(conn); err != nil {
		_ = conn.Close()
		return nil, err
	}
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, req)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_ = conn.Close()
		return nil, fmt.Errorf("proxy CONNECT %s: %s", addr, resp.Status)
	}
	// 若 reader 有缓冲未读数据，需包装
	if br.Buffered() > 0 {
		return &bufConn{Conn: conn, r: br}, nil
	}
	return conn, nil
}

func (d *chromeTLSDialer) dialSOCKS5(ctx context.Context, addr string) (net.Conn, error) {
	var auth *proxy.Auth
	if d.proxyURL.User != nil {
		pass, _ := d.proxyURL.User.Password()
		auth = &proxy.Auth{User: d.proxyURL.User.Username(), Password: pass}
	}
	// golang.org/x/net/proxy Dialer 不直接支持 context，用超时 dial
	base := &net.Dialer{Timeout: d.timeout}
	dialer, err := proxy.SOCKS5("tcp", d.proxyURL.Host, auth, base)
	if err != nil {
		return nil, err
	}
	type contextDialer interface {
		DialContext(ctx context.Context, network, address string) (net.Conn, error)
	}
	if cd, ok := dialer.(contextDialer); ok {
		return cd.DialContext(ctx, "tcp", addr)
	}
	return dialer.Dial("tcp", addr)
}

type bufConn struct {
	net.Conn
	r *bufio.Reader
}

func (c *bufConn) Read(p []byte) (int, error) {
	return c.r.Read(p)
}

const defaultConnPerHost = 4

// buildHTTPClient 构造带 Chrome TLS + 多连接池的 http.Client。
func buildHTTPClient(fp *BrowserFingerprint, pool *ProxyPool) *http.Client {
	var entry *ProxyEntry
	if pool != nil {
		entry = pool.Current()
	}
	perHost := defaultConnPerHost
	if Cfg != nil && Cfg.ConnPerHost > 0 {
		perHost = Cfg.ConnPerHost
	}
	if perHost < 1 {
		perHost = 1
	}
	if perHost > 32 {
		perHost = 32
	}
	dialer := newChromeTLSDialer(entry)
	tr := &http.Transport{
		Proxy:               nil, // TLS 经 DialTLSContext 处理代理隧道
		DialTLSContext:      dialer.DialTLSContext,
		ForceAttemptHTTP2:   false, // HTTP/1.1 多连接；完整 H2 见 docs/HTTP2_JA3.md
		MaxIdleConns:        perHost * 4,
		MaxIdleConnsPerHost: perHost,
		MaxConnsPerHost:     perHost,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 15 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		DisableCompression:    false,
	}
	tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		var nd net.Dialer
		nd.Timeout = 15 * time.Second
		return nd.DialContext(ctx, network, addr)
	}
	_ = fp
	return &http.Client{
		Transport: tr,
		Timeout:   45 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("stopped after 5 redirects")
			}
			return nil
		},
	}
}

// rebuildClientWithProxy 代理切换后重建客户端（新 DialTLS）。
func rebuildClientWithProxy(fp *BrowserFingerprint, pool *ProxyPool) *http.Client {
	return buildHTTPClient(fp, pool)
}

func hostPortFromURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if u.Port() != "" {
		return u.Host
	}
	if strings.EqualFold(u.Scheme, "https") {
		return net.JoinHostPort(u.Hostname(), "443")
	}
	return net.JoinHostPort(u.Hostname(), "80")
}
