package worker

import (
	"biliTickerStorm/internal/common/workercfg"
	"strings"
	"sync/atomic"
	"time"
)

// remoteConfigVersion 已应用的 master 配置版本。
var remoteConfigVersion atomic.Int64

// ApplyRemoteSettings 合并 master 下发的 settings（非空覆盖 env 默认）。
func ApplyRemoteSettings(jsonStr string, version int64) {
	if version > 0 && version == remoteConfigVersion.Load() {
		return
	}
	if jsonStr == "" || jsonStr == "{}" || jsonStr == "null" {
		if version > 0 {
			remoteConfigVersion.Store(version)
		}
		return
	}
	s, err := workercfg.FromJSON([]byte(jsonStr))
	if err != nil {
		log.Warnf("解析 master worker 配置失败: %v", err)
		return
	}
	cfg := GetConfig()
	if cfg == nil {
		return
	}
	// 复制一份再改
	next := *cfg
	applySettingsToConfig(&next, s)
	SetConfig(&next)
	remoteConfigVersion.Store(version)
	log.Infof("已应用 master worker 配置 version=%d channels=%v", version, notifyConfigFromEnv().EnabledChannels())
}

func applySettingsToConfig(cfg *Config, s workercfg.Settings) {
	if v := stringsTrim(s.PushplusToken); v != "" {
		cfg.PushplusToken = v
	}
	if v := stringsTrim(s.BarkToken); v != "" {
		cfg.BarkToken = v
	}
	if v := stringsTrim(s.ServerChanKey); v != "" {
		cfg.ServerChanKey = v
	}
	if v := stringsTrim(s.ServerChan3APIURL); v != "" {
		cfg.ServerChan3APIURL = v
	}
	if v := stringsTrim(s.TelegramBotToken); v != "" {
		cfg.TelegramBotToken = v
	}
	if v := stringsTrim(s.TelegramChatID); v != "" {
		cfg.TelegramChatID = v
	}
	if v := stringsTrim(s.TelegramHTTPProxy); v != "" {
		cfg.TelegramHTTPProxy = v
	}
	// 允许清空：settings 里显式有 key 时——JSON 省略与空难区分；MVP 非空才覆盖
	if s.Interval != nil && *s.Interval > 0 {
		cfg.Interval = *s.Interval
	}
	if s.RateLimitDelayMs != nil && *s.RateLimitDelayMs >= 0 {
		cfg.RateLimitDelayMs = *s.RateLimitDelayMs
	}
	if s.RiskLocalRetries != nil && *s.RiskLocalRetries > 0 {
		cfg.RiskLocalRetries = *s.RiskLocalRetries
	}
	if s.RiskCooldownBaseMs != nil && *s.RiskCooldownBaseMs > 0 {
		cfg.RiskCooldownBaseMs = *s.RiskCooldownBaseMs
	}
	if s.RiskCooldownMaxSec != nil && *s.RiskCooldownMaxSec > 0 {
		cfg.RiskCooldownMaxSec = *s.RiskCooldownMaxSec
	}
	// 代理列表：非空覆盖；若要清空需传特殊标记——暂用非空覆盖
	if stringsTrim(s.ProxyList) != "" {
		cfg.ProxyList = s.ProxyList
	}
	if s.ProxyMaxFails != nil && *s.ProxyMaxFails > 0 {
		cfg.ProxyMaxFails = *s.ProxyMaxFails
	}
	if s.ProxyCooldownSec != nil && *s.ProxyCooldownSec > 0 {
		cfg.ProxyCooldownSec = *s.ProxyCooldownSec
	}
	if s.ProxyMaxBackoffSec != nil && *s.ProxyMaxBackoffSec > 0 {
		cfg.ProxyMaxBackoffSec = *s.ProxyMaxBackoffSec
	}
	if stringsTrim(s.ProxyAPIURL) != "" {
		cfg.ProxyAPIURL = s.ProxyAPIURL
	}
	if s.ProxyAPICount != nil && *s.ProxyAPICount > 0 {
		cfg.ProxyAPICount = *s.ProxyAPICount
	}
	if stringsTrim(s.ProxyAPIScheme) != "" {
		cfg.ProxyAPIScheme = s.ProxyAPIScheme
	}
	if s.ConnPerHost != nil && *s.ConnPerHost > 0 {
		cfg.ConnPerHost = *s.ConnPerHost
		if cfg.ConnPerHost > 32 {
			cfg.ConnPerHost = 32
		}
	}
	if s.CreateBatchSize != nil && *s.CreateBatchSize > 0 {
		cfg.CreateBatchSize = *s.CreateBatchSize
	}
	if s.EnableWarmup != nil {
		cfg.EnableWarmup = *s.EnableWarmup
	}
	if v := stringsTrim(s.TicketTimeStart); v != "" {
		cfg.TimeStartRaw = v
		loc, _ := time.LoadLocation("Asia/Shanghai")
		if t, err := time.ParseInLocation("2006-01-02T15:04", v, loc); err == nil {
			cfg.TimeStart = &t
		}
	}
	if cfg.CreateBatchSize > cfg.ConnPerHost && cfg.ConnPerHost > 0 {
		cfg.CreateBatchSize = cfg.ConnPerHost
	}
}

func stringsTrim(s string) string {
	return strings.TrimSpace(s)
}
