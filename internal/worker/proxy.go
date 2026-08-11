package worker

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const proxyDirectLabel = "none"

// ProxyEntry 单条代理。
type ProxyEntry struct {
	Raw           string
	URL           *url.URL
	Kind          string // http / socks5
	ConsecutiveFail int
	CooldownUntil time.Time
}

// MaskProxy 脱敏展示：隐藏密码。
func MaskProxy(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == proxyDirectLabel {
		return proxyDirectLabel
	}
	u, err := url.Parse(normalizeProxyRaw(raw))
	if err != nil || u.Host == "" {
		return "***"
	}
	if u.User != nil {
		user := u.User.Username()
		if user == "" {
			user = "***"
		}
		u.User = url.UserPassword(user, "***")
	}
	return u.String()
}

func normalizeProxyRaw(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "://") {
		return "http://" + raw
	}
	return raw
}

func parseProxyEntry(raw string) (*ProxyEntry, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.EqualFold(raw, proxyDirectLabel) {
		return nil, nil
	}
	norm := normalizeProxyRaw(raw)
	u, err := url.Parse(norm)
	if err != nil {
		return nil, fmt.Errorf("invalid proxy %q: %w", raw, err)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("invalid proxy host: %q", raw)
	}
	kind := strings.ToLower(u.Scheme)
	switch kind {
	case "http", "https":
		kind = "http"
	case "socks5", "socks5h":
		kind = "socks5"
	default:
		return nil, fmt.Errorf("unsupported proxy scheme %q", u.Scheme)
	}
	return &ProxyEntry{Raw: raw, URL: u, Kind: kind}, nil
}

// ParseProxyList 解析逗号/分号/换行分隔的代理列表。
func ParseProxyList(raw string) ([]*ProxyEntry, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n' || r == '\r'
	})
	var out []*ProxyEntry
	seen := map[string]struct{}{}
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		e, err := parseProxyEntry(p)
		if err != nil {
			return nil, err
		}
		if e == nil {
			continue
		}
		key := e.URL.String()
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, e)
	}
	return out, nil
}

// ProxyPoolConfig 代理池参数。
type ProxyPoolConfig struct {
	List          string
	MaxFails      int
	CooldownSec   int
	MaxBackoffSec int
	APIURL        string
	APICount      int
	APIScheme     string // http / socks5，用于 API 返回的 host:port
}

// ProxyPool 轮换 / 冷却 / 恢复。
type ProxyPool struct {
	mu            sync.Mutex
	entries       []*ProxyEntry
	idx           int
	maxFails      int
	cooldownSec   int
	maxBackoffSec int
	apiURL        string
	apiCount      int
	apiScheme     string
	// 无配置代理时恒为直连
	directOnly bool
}

func NewProxyPool(cfg ProxyPoolConfig) (*ProxyPool, error) {
	if cfg.MaxFails <= 0 {
		cfg.MaxFails = 3
	}
	if cfg.CooldownSec <= 0 {
		cfg.CooldownSec = 30
	}
	if cfg.MaxBackoffSec <= 0 {
		cfg.MaxBackoffSec = 300
	}
	if cfg.APICount <= 0 {
		cfg.APICount = 5
	}
	if cfg.APIScheme == "" {
		cfg.APIScheme = "http"
	}
	entries, err := ParseProxyList(cfg.List)
	if err != nil {
		return nil, err
	}
	p := &ProxyPool{
		entries:       entries,
		maxFails:      cfg.MaxFails,
		cooldownSec:   cfg.CooldownSec,
		maxBackoffSec: cfg.MaxBackoffSec,
		apiURL:        strings.TrimSpace(cfg.APIURL),
		apiCount:      cfg.APICount,
		apiScheme:     strings.ToLower(cfg.APIScheme),
		directOnly:    len(entries) == 0 && strings.TrimSpace(cfg.APIURL) == "",
	}
	return p, nil
}

func (p *ProxyPool) HasConfiguredProxies() bool {
	if p == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return !p.directOnly || len(p.entries) > 0 || p.apiURL != ""
}

// Current 返回当前可用代理；nil 表示直连。
func (p *ProxyPool) Current() *ProxyEntry {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.currentLocked(time.Now())
}

func (p *ProxyPool) currentLocked(now time.Time) *ProxyEntry {
	if len(p.entries) == 0 {
		return nil
	}
	n := len(p.entries)
	for i := 0; i < n; i++ {
		idx := (p.idx + i) % n
		e := p.entries[idx]
		if e.CooldownUntil.After(now) {
			continue
		}
		p.idx = idx
		return e
	}
	return nil
}

// CurrentLabel 脱敏标签。
func (p *ProxyPool) CurrentLabel() string {
	e := p.Current()
	if e == nil {
		return proxyDirectLabel
	}
	return MaskProxy(e.Raw)
}

// MarkSuccess 重置当前代理连续失败。
func (p *ProxyPool) MarkSuccess() {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if e := p.currentLocked(time.Now()); e != nil {
		e.ConsecutiveFail = 0
	}
}

// MarkFailure 标记当前代理失败并尝试切换。
// switched: 已切到另一条可用代理
// exhausted: 配置了代理但当前全部不可用
func (p *ProxyPool) MarkFailure(reason string) (switched bool, exhausted bool) {
	if p == nil {
		return false, false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	cur := p.currentLocked(now)
	if cur == nil {
		// 直连或全部冷却
		if len(p.entries) == 0 {
			return false, false
		}
		return false, true
	}
	cur.ConsecutiveFail++
	failN := cur.ConsecutiveFail
	log.Warnf("代理失败 proxy=%s reason=%s fails=%d/%d", MaskProxy(cur.Raw), reason, failN, p.maxFails)
	if failN >= p.maxFails {
		backoff := p.cooldownSec * failN
		if backoff > p.maxBackoffSec {
			backoff = p.maxBackoffSec
		}
		cur.CooldownUntil = now.Add(time.Duration(backoff) * time.Second)
		log.Warnf("代理进入冷却 proxy=%s cooldown=%ds", MaskProxy(cur.Raw), backoff)
		// 切到下一条
		p.idx = (p.idx + 1) % len(p.entries)
	} else {
		// 未达阈值也轮换，避免单点连续 412
		p.idx = (p.idx + 1) % len(p.entries)
	}
	next := p.currentLocked(time.Now())
	if next == nil {
		log.Warn("代理池耗尽：全部冷却或不可用")
		return false, true
	}
	if next == cur {
		// 仅一条代理且未冷却完毕
		if next.CooldownUntil.After(time.Now()) {
			return false, true
		}
		return false, false
	}
	log.Infof("切换代理 -> %s", MaskProxy(next.Raw))
	return true, false
}

// AvailableCount 当前未冷却代理数。
func (p *ProxyPool) AvailableCount() int {
	if p == nil {
		return 0
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	n := 0
	for _, e := range p.entries {
		if !e.CooldownUntil.After(now) {
			n++
		}
	}
	return n
}

// StatusSnapshot 供日志/上报。
func (p *ProxyPool) StatusSnapshot() map[string]interface{} {
	if p == nil {
		return map[string]interface{}{
			"current": proxyDirectLabel,
			"total":   0,
			"available": 0,
		}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	avail := 0
	var items []map[string]interface{}
	for _, e := range p.entries {
		cool := e.CooldownUntil.After(now)
		if !cool {
			avail++
		}
		items = append(items, map[string]interface{}{
			"proxy":    MaskProxy(e.Raw),
			"fails":    e.ConsecutiveFail,
			"cooldown": cool,
		})
	}
	cur := proxyDirectLabel
	if e := p.currentLocked(now); e != nil {
		cur = MaskProxy(e.Raw)
	}
	return map[string]interface{}{
		"current":   cur,
		"total":     len(p.entries),
		"available": avail,
		"api":       p.apiURL != "",
		"entries":   items,
	}
}

// RefreshFromAPI 从代理 API 拉取并合并进池。
func (p *ProxyPool) RefreshFromAPI() (int, error) {
	if p == nil {
		return 0, nil
	}
	p.mu.Lock()
	api := p.apiURL
	count := p.apiCount
	scheme := p.apiScheme
	p.mu.Unlock()
	if api == "" {
		return 0, nil
	}
	fetchURL := api
	if strings.Contains(api, "%d") {
		fetchURL = fmt.Sprintf(api, count)
	}
	resp, err := http.Get(fetchURL)
	if err != nil {
		return 0, fmt.Errorf("proxy api: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return 0, err
	}
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("proxy api HTTP %d: %s", resp.StatusCode, summarizeNonJSON(body))
	}
	lines := parseProxyAPIBody(body, scheme)
	if len(lines) == 0 {
		return 0, fmt.Errorf("proxy api empty result")
	}
	added := 0
	p.mu.Lock()
	defer p.mu.Unlock()
	seen := map[string]struct{}{}
	for _, e := range p.entries {
		seen[e.URL.String()] = struct{}{}
	}
	for _, raw := range lines {
		e, err := parseProxyEntry(raw)
		if err != nil || e == nil {
			continue
		}
		key := e.URL.String()
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		p.entries = append(p.entries, e)
		added++
	}
	if added > 0 {
		p.directOnly = false
		log.Infof("代理 API 新增 %d 条，池大小 %d", added, len(p.entries))
	}
	return added, nil
}

func parseProxyAPIBody(body []byte, scheme string) []string {
	s := strings.TrimSpace(string(body))
	if s == "" {
		return nil
	}
	// JSON 数组 ["ip:port", ...] 或 {"data":[...]} / {"proxies":[...]}
	if strings.HasPrefix(s, "[") || strings.HasPrefix(s, "{") {
		var arr []string
		if err := json.Unmarshal(body, &arr); err == nil {
			return normalizeAPIList(arr, scheme)
		}
		var obj map[string]interface{}
		if err := json.Unmarshal(body, &obj); err == nil {
			for _, key := range []string{"data", "proxies", "list", "result"} {
				if v, ok := obj[key]; ok {
					return normalizeAPIAny(v, scheme)
				}
			}
		}
	}
	// 纯文本：一行一个
	var lines []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lines = append(lines, ensureProxyScheme(line, scheme))
	}
	return lines
}

func normalizeAPIAny(v interface{}, scheme string) []string {
	switch t := v.(type) {
	case []interface{}:
		var out []string
		for _, item := range t {
			if s, ok := item.(string); ok {
				out = append(out, ensureProxyScheme(s, scheme))
			}
		}
		return out
	case []string:
		return normalizeAPIList(t, scheme)
	default:
		return nil
	}
}

func normalizeAPIList(arr []string, scheme string) []string {
	var out []string
	for _, s := range arr {
		out = append(out, ensureProxyScheme(s, scheme))
	}
	return out
}

func ensureProxyScheme(raw, scheme string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return raw
	}
	if strings.Contains(raw, "://") {
		return raw
	}
	if scheme == "" {
		scheme = "http"
	}
	return scheme + "://" + raw
}

// DialAddr 供 fasthttp 使用：http 为 user:pass@host:port；socks5 为完整 URL。
func (e *ProxyEntry) DialAddr() string {
	if e == nil || e.URL == nil {
		return ""
	}
	if e.Kind == "socks5" {
		return e.URL.String()
	}
	// HTTP 代理：fasthttpproxy 要 host:port 或 user:pass@host:port
	if e.URL.User != nil {
		pass, _ := e.URL.User.Password()
		return fmt.Sprintf("%s:%s@%s", e.URL.User.Username(), pass, e.URL.Host)
	}
	return e.URL.Host
}
