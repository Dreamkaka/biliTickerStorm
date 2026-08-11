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
	"golang.org/x/net/http2"
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

// utlsConn 将 *utls.UConn 适配为 crypto/tls.ConnectionState，供 net/http 与 x/net/http2 读取 ALPN。
type utlsConn struct {
	*utls.UConn
}

func (c *utlsConn) ConnectionState() tls.ConnectionState {
	cs := c.UConn.ConnectionState()
	return tls.ConnectionState{
		Version:                     cs.Version,
		HandshakeComplete:           cs.HandshakeComplete,
		DidResume:                   cs.DidResume,
		CipherSuite:                 cs.CipherSuite,
		NegotiatedProtocol:          cs.NegotiatedProtocol,
		NegotiatedProtocolIsMutual:  true,
		ServerName:                  cs.ServerName,
		PeerCertificates:            cs.PeerCertificates,
		VerifiedChains:              cs.VerifiedChains,
		SignedCertificateTimestamps: cs.SignedCertificateTimestamps,
		OCSPResponse:                cs.OCSPResponse,
		TLSUnique:                   cs.TLSUnique,
	}
}

func (d *chromeTLSDialer) DialTLSContext(ctx context.Context, network, addr string) (net.Conn, error) {
	return d.dialUTLS(ctx, addr)
}

// dialH2TLS 供 http2.Transport：握手后必须 ALPN=h2（自定义 Dial 时 x/net/http2 不校验 ALPN）。
func (d *chromeTLSDialer) dialH2TLS(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
	c, err := d.dialUTLS(ctx, addr)
	if err != nil {
		return nil, err
	}
	if p := negotiatedALPN(c); p != "h2" {
		_ = c.Close()
		return nil, fmt.Errorf("http2: unexpected ALPN protocol %q; want %q", p, "h2")
	}
	return c, nil
}

// dialH1TLS 供 HTTP/1.1 Transport：若协商到 h2 则拒绝，避免 malformed HTTP response。
func (d *chromeTLSDialer) dialH1TLS(ctx context.Context, network, addr string) (net.Conn, error) {
	c, err := d.dialUTLS(ctx, addr)
	if err != nil {
		return nil, err
	}
	if p := negotiatedALPN(c); p == "h2" {
		_ = c.Close()
		return nil, fmt.Errorf("http2: unexpected ALPN protocol %q for HTTP/1.1 transport", p)
	}
	return c, nil
}

func negotiatedALPN(c net.Conn) string {
	type stater interface {
		ConnectionState() tls.ConnectionState
	}
	if s, ok := c.(stater); ok {
		return s.ConnectionState().NegotiatedProtocol
	}
	return ""
}

func (d *chromeTLSDialer) dialUTLS(ctx context.Context, addr string) (net.Conn, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	raw, err := d.dialTCP(ctx, addr)
	if err != nil {
		return nil, err
	}
	// 与 Chrome 一致：优先 h2，回退 http/1.1
	cfg := &utls.Config{
		ServerName:         host,
		InsecureSkipVerify: false,
		MinVersion:         tls.VersionTLS12,
		NextProtos:         []string{"h2", "http/1.1"},
	}
	uconn := utls.UClient(raw, cfg, utls.HelloChrome_Auto)
	spec, err := utls.UTLSIdToSpec(utls.HelloChrome_Auto)
	if err == nil {
		for i := range spec.Extensions {
			if alpn, ok := spec.Extensions[i].(*utls.ALPNExtension); ok {
				alpn.AlpnProtocols = []string{"h2", "http/1.1"}
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
	return &utlsConn{UConn: uconn}, nil
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

// alpnTransport 按 TLS ALPN 选择 h2 / HTTP/1.1；uTLS 拨号后 net/http 无法自动升级 h2。
type alpnTransport struct {
	dialer *chromeTLSDialer
	h1     *http.Transport
	h2     *http2.Transport
}

func (t *alpnTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL == nil || req.URL.Scheme != "https" {
		return t.h1.RoundTrip(req)
	}
	// 优先走 HTTP/2（show.bilibili.com 等强制 h2 时必须）
	resp, err := t.h2.RoundTrip(req)
	if err == nil {
		return resp, nil
	}
	// ALPN 非 h2 时 http2.Transport 报 unexpected ALPN；回退 HTTP/1.1
	if isUnexpectedALPN(err) {
		return t.h1.RoundTrip(req)
	}
	return nil, err
}

func isUnexpectedALPN(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "unexpected ALPN")
}

func (t *alpnTransport) CloseIdleConnections() {
	t.h1.CloseIdleConnections()
	t.h2.CloseIdleConnections()
}

// buildHTTPClient 构造带 Chrome TLS + HTTP/2（优先）+ HTTP/1.1 回退的 http.Client。
func buildHTTPClient(fp *BrowserFingerprint, pool *ProxyPool) *http.Client {
	var entry *ProxyEntry
	if pool != nil {
		entry = pool.Current()
	}
	perHost := defaultConnPerHost
	cfg := GetConfig()
	if cfg != nil && cfg.ConnPerHost > 0 {
		perHost = cfg.ConnPerHost
	}
	if perHost < 1 {
		perHost = 1
	}
	if perHost > 32 {
		perHost = 32
	}
	dialer := newChromeTLSDialer(entry)

	h1 := &http.Transport{
		Proxy:                 nil, // TLS 经 DialTLSContext 处理代理隧道
		DialTLSContext:        dialer.dialH1TLS,
		ForceAttemptHTTP2:     false, // 由 alpnTransport 显式走 h2
		TLSNextProto:          map[string]func(string, *tls.Conn) http.RoundTripper{},
		MaxIdleConns:          perHost * 4,
		MaxIdleConnsPerHost:   perHost,
		MaxConnsPerHost:       perHost,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		DisableCompression:    false,
	}
	h1.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		var nd net.Dialer
		nd.Timeout = 15 * time.Second
		return nd.DialContext(ctx, network, addr)
	}

	h2 := &http2.Transport{
		// 忽略传入 tls.Config，统一走 uTLS Chrome 指纹；拨号后强制 ALPN=h2
		DialTLSContext:     dialer.dialH2TLS,
		AllowHTTP:          false,
		DisableCompression: false,
		IdleConnTimeout:    90 * time.Second,
		ReadIdleTimeout:    30 * time.Second,
		PingTimeout:        15 * time.Second,
	}

	_ = fp
	return &http.Client{
		Transport: &alpnTransport{dialer: dialer, h1: h1, h2: h2},
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
