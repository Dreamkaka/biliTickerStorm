package worker

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var (
	ErrRiskControl = errors.New("412风控")
	ErrRateLimit   = errors.New("429请求过多")
)

type BiliClient struct {
	httpClient *http.Client
	cookies    []Cookies
	worker     *Worker
	pool       *ProxyPool
	fp         BrowserFingerprint
}

func NewBiliClient(cookies []Cookies, worker *Worker) *BiliClient {
	var pool *ProxyPool
	if Cfg != nil {
		p, err := NewProxyPool(ProxyPoolConfig{
			List:          Cfg.ProxyList,
			MaxFails:      Cfg.ProxyMaxFails,
			CooldownSec:   Cfg.ProxyCooldownSec,
			MaxBackoffSec: Cfg.ProxyMaxBackoffSec,
			APIURL:        Cfg.ProxyAPIURL,
			APICount:      Cfg.ProxyAPICount,
			APIScheme:     Cfg.ProxyAPIScheme,
		})
		if err != nil {
			log.Warnf("代理池初始化失败，回退直连: %v", err)
		} else {
			pool = p
			if pool.HasConfiguredProxies() {
				log.Infof("代理池已启用: %v", pool.StatusSnapshot())
			}
		}
	}
	fp := NewBrowserFingerprint()
	bc := &BiliClient{
		cookies: cookies,
		worker:  worker,
		pool:    pool,
		fp:      fp,
	}
	bc.httpClient = buildHTTPClient(&bc.fp, pool)
	log.Infof("浏览器指纹 UA=%s", bc.fp.UserAgent)
	return bc
}

func (bc *BiliClient) Fingerprint() BrowserFingerprint {
	return bc.fp
}

func (bc *BiliClient) getCookieValue(name string) string {
	for _, cookie := range bc.cookies {
		if strings.EqualFold(cookie.Name, name) {
			return cookie.Value
		}
	}
	return ""
}

func (bc *BiliClient) cookieHeader() string {
	var parts []string
	for _, c := range bc.cookies {
		if c.Domain == ".bilibili.com" || strings.HasSuffix(c.Domain, "bilibili.com") || c.Domain == "" {
			parts = append(parts, c.Name+"="+c.Value)
		}
	}
	return strings.Join(parts, "; ")
}

func (bc *BiliClient) applyProxyDial() {
	// 代理切换后重建 Transport（uTLS DialTLSContext 绑定当前代理）
	bc.httpClient = rebuildClientWithProxy(&bc.fp, bc.pool)
}

// handleProxyRisk 在 412 时标记代理失败并尝试切换；返回是否已切换到可用代理。
func (bc *BiliClient) handleProxyRisk(reason string) (switched bool, exhausted bool) {
	if bc.pool == nil || !bc.pool.HasConfiguredProxies() {
		return false, false
	}
	switched, exhausted = bc.pool.MarkFailure(reason)
	if exhausted {
		if n, err := bc.pool.RefreshFromAPI(); err != nil {
			log.Warnf("代理 API 刷新失败: %v", err)
		} else if n > 0 && bc.pool.Current() != nil {
			switched = true
			exhausted = false
			log.Infof("代理 API 刷新后恢复，当前: %s", bc.pool.CurrentLabel())
		}
	}
	bc.applyProxyDial()
	if switched {
		log.Infof("当前代理: %s", bc.pool.CurrentLabel())
	}
	return switched, exhausted
}

func (bc *BiliClient) markProxySuccess() {
	if bc.pool != nil {
		bc.pool.MarkSuccess()
	}
}

func (bc *BiliClient) proxyLabel() string {
	if bc.pool == nil {
		return proxyDirectLabel
	}
	return bc.pool.CurrentLabel()
}

func (bc *BiliClient) setRequestHeaders(req *http.Request, contentType string) {
	bc.fp.ApplyJSONHeaders(req.Header.Set, contentType, "https://show.bilibili.com/")
	if ck := bc.cookieHeader(); ck != "" {
		req.Header.Set("Cookie", ck)
	}
	// 不设置 Connection/User-Agent 以外的 Go 默认头；Transport 不自动加 Go-http-client
}

func (bc *BiliClient) do(req *http.Request) ([]byte, error) {
	resp, err := bc.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := readResponseBody(resp)
	if err != nil {
		return nil, err
	}
	if err := bc.handleHTTPStatus(resp.StatusCode, resp.Header.Get("Content-Type"), body); err != nil {
		return nil, err
	}
	return body, nil
}

func readResponseBody(resp *http.Response) ([]byte, error) {
	var r io.Reader = resp.Body
	if strings.EqualFold(resp.Header.Get("Content-Encoding"), "gzip") {
		gr, err := gzip.NewReader(resp.Body)
		if err != nil {
			return nil, err
		}
		defer gr.Close()
		r = gr
	}
	return io.ReadAll(io.LimitReader(r, 8<<20))
}

func (bc *BiliClient) Get(rawURL string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	bc.setRequestHeaders(req, "")
	req.Header.Del("Content-Type")
	return bc.do(req)
}

func (bc *BiliClient) Post(rawURL string, data interface{}) ([]byte, error) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, rawURL, bytes.NewReader(jsonData))
	if err != nil {
		return nil, err
	}
	bc.setRequestHeaders(req, "application/json")
	return bc.do(req)
}

func (bc *BiliClient) DoFormRequest(rawURL string, data map[string]string) ([]byte, error) {
	form := url.Values{}
	for k, v := range data {
		form.Set(k, v)
	}
	req, err := http.NewRequest(http.MethodPost, rawURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	bc.setRequestHeaders(req, "application/x-www-form-urlencoded")
	return bc.do(req)
}

func (bc *BiliClient) handleHTTPStatus(status int, contentType string, body []byte) error {
	switch status {
	case http.StatusOK:
		return nil
	case http.StatusPreconditionFailed:
		log.Warnf("HTTP 412 风控 proxy=%s", bc.proxyLabel())
		return ErrRiskControl
	case http.StatusTooManyRequests:
		return ErrRateLimit
	default:
		return fmt.Errorf("%s", diagnoseHTTPError(status, contentType, body))
	}
}

func diagnoseHTTPError(status int, contentType string, body []byte) string {
	preview := string(body)
	preview = strings.ReplaceAll(preview, "\r", "\\r")
	preview = strings.ReplaceAll(preview, "\n", "\\n")
	if len(preview) > 200 {
		preview = preview[:200] + "..."
	}
	if preview == "" {
		preview = "<empty>"
	}
	if contentType == "" {
		contentType = "<none>"
	}
	return fmt.Sprintf("HTTP %d content-type=%s body=%s", status, contentType, preview)
}

// 保留 time 引用给可能的扩展
var _ = time.Second
