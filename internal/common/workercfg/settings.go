package workercfg

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Settings 集群级 worker 运行参数（WebUI 可写；env 作默认，非空字段覆盖）。
// 不含 MASTER_SERVER_ADDR 等部署级变量。
type Settings struct {
	// 通知
	PushplusToken     string `json:"pushplus_token,omitempty"`
	BarkToken         string `json:"bark_token,omitempty"`
	ServerChanKey     string `json:"serverchan_key,omitempty"`
	ServerChan3APIURL string `json:"serverchan3_api_url,omitempty"`
	TelegramBotToken  string `json:"telegram_bot_token,omitempty"`
	TelegramChatID    string `json:"telegram_chat_id,omitempty"`
	TelegramHTTPProxy string `json:"telegram_http_proxy,omitempty"`

	// 节奏 / 风控
	Interval           *int `json:"interval_ms,omitempty"`
	RateLimitDelayMs   *int `json:"rate_limit_delay_ms,omitempty"`
	RiskLocalRetries   *int `json:"risk_local_retries,omitempty"`
	RiskCooldownBaseMs *int `json:"risk_cooldown_base_ms,omitempty"`
	RiskCooldownMaxSec *int `json:"risk_cooldown_max_sec,omitempty"`

	// 代理
	ProxyList          string `json:"proxy_list,omitempty"`
	ProxyMaxFails      *int  `json:"proxy_max_fails,omitempty"`
	ProxyCooldownSec   *int  `json:"proxy_cooldown_sec,omitempty"`
	ProxyMaxBackoffSec *int  `json:"proxy_max_backoff_sec,omitempty"`
	ProxyAPIURL        string `json:"proxy_api_url,omitempty"`
	ProxyAPICount      *int  `json:"proxy_api_count,omitempty"`
	ProxyAPIScheme     string `json:"proxy_api_scheme,omitempty"`

	// 连接
	ConnPerHost     *int  `json:"conn_per_host,omitempty"`
	CreateBatchSize *int  `json:"create_batch_size,omitempty"`
	EnableWarmup    *bool `json:"enable_warmup,omitempty"`

	// 定时（可选全局）
	TicketTimeStart string `json:"ticket_time_start,omitempty"`
}

func FilePath(configPath string) string {
	return filepath.Join(configPath, "worker_settings.json")
}

func Load(configPath string) (Settings, int64, error) {
	path := FilePath(configPath)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Settings{}, 0, nil
		}
		return Settings{}, 0, err
	}
	var s Settings
	if err := json.Unmarshal(data, &s); err != nil {
		return Settings{}, 0, err
	}
	var ver int64
	if st, err := os.Stat(path); err == nil {
		ver = st.ModTime().UnixNano()
	}
	if ver == 0 {
		ver = time.Now().UnixNano()
	}
	return s, ver, nil
}

func Save(configPath string, s Settings) (int64, error) {
	if err := os.MkdirAll(configPath, 0o755); err != nil {
		return 0, err
	}
	path := FilePath(configPath)
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return 0, err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return 0, err
	}
	st, err := os.Stat(path)
	if err != nil {
		return time.Now().UnixNano(), nil
	}
	return st.ModTime().UnixNano(), nil
}

// Masked 返回脱敏副本（用于 GET API）。
func (s Settings) Masked() Settings {
	out := s
	out.PushplusToken = maskSecret(s.PushplusToken)
	out.BarkToken = maskSecret(s.BarkToken)
	out.ServerChanKey = maskSecret(s.ServerChanKey)
	out.TelegramBotToken = maskSecret(s.TelegramBotToken)
	// ServerChan3 URL / Telegram proxy 可能含密钥，轻度脱敏
	if strings.Contains(out.ServerChan3APIURL, "http") && len(out.ServerChan3APIURL) > 24 {
		out.ServerChan3APIURL = out.ServerChan3APIURL[:16] + "***"
	}
	if out.TelegramHTTPProxy != "" {
		out.TelegramHTTPProxy = maskSecret(out.TelegramHTTPProxy)
	}
	// 代理列表脱敏密码
	if out.ProxyList != "" {
		out.ProxyList = maskProxyList(out.ProxyList)
	}
	return out
}

func maskSecret(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if len(s) <= 4 {
		return "****"
	}
	return s[:2] + "****" + s[len(s)-2:]
}

func maskProxyList(raw string) string {
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n'
	})
	for i, p := range parts {
		p = strings.TrimSpace(p)
		if u, err := parseLooseURL(p); err == nil && u != "" {
			parts[i] = u
		}
	}
	return strings.Join(parts, ",")
}

func parseLooseURL(raw string) (string, error) {
	// 简单隐藏 user:pass@
	if i := strings.Index(raw, "://"); i >= 0 {
		rest := raw[i+3:]
		if at := strings.Index(rest, "@"); at > 0 {
			userinfo := rest[:at]
			host := rest[at+1:]
			if colon := strings.Index(userinfo, ":"); colon >= 0 {
				userinfo = userinfo[:colon] + ":***"
			}
			return raw[:i+3] + userinfo + "@" + host, nil
		}
	}
	return raw, nil
}

// Normalize 校验并修正边界。
func (s *Settings) Normalize() {
	clampInt := func(p **int, min, max int) {
		if p == nil || *p == nil {
			return
		}
		v := **p
		if v < min {
			v = min
		}
		if max > 0 && v > max {
			v = max
		}
		**p = v
	}
	clampInt(&s.Interval, 1, 0)
	clampInt(&s.RateLimitDelayMs, 0, 0)
	clampInt(&s.RiskLocalRetries, 1, 100)
	clampInt(&s.RiskCooldownBaseMs, 100, 0)
	clampInt(&s.RiskCooldownMaxSec, 1, 3600)
	clampInt(&s.ProxyMaxFails, 1, 100)
	clampInt(&s.ProxyCooldownSec, 1, 0)
	clampInt(&s.ProxyMaxBackoffSec, 1, 0)
	clampInt(&s.ProxyAPICount, 1, 100)
	clampInt(&s.ConnPerHost, 1, 32)
	clampInt(&s.CreateBatchSize, 1, 32)
	if s.ConnPerHost != nil && s.CreateBatchSize != nil && *s.CreateBatchSize > *s.ConnPerHost {
		v := *s.ConnPerHost
		s.CreateBatchSize = &v
	}
}

// ToJSON 序列化完整配置（下发用，不脱敏）。
func (s Settings) ToJSON() ([]byte, error) {
	return json.Marshal(s)
}

func FromJSON(data []byte) (Settings, error) {
	var s Settings
	if len(data) == 0 {
		return s, nil
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return s, err
	}
	s.Normalize()
	return s, nil
}
