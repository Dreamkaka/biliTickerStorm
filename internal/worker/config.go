package worker

import (
	"fmt"
	"sync/atomic"
	"time"

	"github.com/caarlos0/env/v10"
)

type Config struct {
	MasterServerAddr   string     `env:"MASTER_SERVER_ADDR"`
	TimeStartRaw       string     `env:"TICKET_TIME_START"`
	TimeStart          *time.Time
	PushplusToken      string `env:"PUSHPLUS_TOKEN"`
	BarkToken          string `env:"BARK_TOKEN"`
	ServerChanKey      string `env:"SERVERCHAN_KEY"`
	ServerChan3APIURL  string `env:"SERVERCHAN3_API_URL"`
	TelegramBotToken   string `env:"TELEGRAM_BOT_TOKEN"`
	TelegramChatID     string `env:"TELEGRAM_CHAT_ID"`
	TelegramHTTPProxy  string `env:"TELEGRAM_HTTP_PROXY"`
	Interval           int    `env:"TICKET_INTERVAL" envDefault:"300"`
	RateLimitDelayMs   int    `env:"RATE_LIMIT_DELAY_MS" envDefault:"300"`
	RiskLocalRetries   int    `env:"RISK_LOCAL_RETRIES" envDefault:"5"`
	RiskCooldownBaseMs int    `env:"RISK_COOLDOWN_BASE_MS" envDefault:"1000"`
	RiskCooldownMaxSec int    `env:"RISK_COOLDOWN_MAX_SEC" envDefault:"30"`
	ProxyList          string `env:"PROXY_LIST"`
	ProxyMaxFails      int    `env:"PROXY_MAX_FAILS" envDefault:"3"`
	ProxyCooldownSec   int    `env:"PROXY_COOLDOWN_SEC" envDefault:"30"`
	ProxyMaxBackoffSec int    `env:"PROXY_MAX_BACKOFF_SEC" envDefault:"300"`
	ProxyAPIURL        string `env:"PROXY_API_URL"`
	ProxyAPICount      int    `env:"PROXY_API_COUNT" envDefault:"5"`
	ProxyAPIScheme     string `env:"PROXY_API_SCHEME" envDefault:"http"`
	ConnPerHost        int    `env:"CONN_PER_HOST" envDefault:"4"`
	CreateBatchSize    int    `env:"CREATE_BATCH_SIZE" envDefault:"2"`
	EnableWarmup       bool   `env:"ENABLE_WARMUP" envDefault:"true"`
}

var cfgAtomic atomic.Pointer[Config]

// Cfg 当前配置快照；热更新后会替换。读配置请优先 GetConfig()。
var Cfg *Config

func init() {
	Cfg = LoadConfig()
	cfgAtomic.Store(Cfg)
}

func LoadConfig() *Config {
	cfg := &Config{}
	if err := env.Parse(cfg); err != nil {
		log.Fatalf("环境变量解析失败: %v", err)
	}
	if cfg.MasterServerAddr == "" {
		log.Fatalf("❌ MASTER_SERVER_ADDR 是必需的环境变量，当前未设置")
	}
	normalizeConfig(cfg)
	nc := NotifyConfig{
		PushplusToken:     cfg.PushplusToken,
		BarkToken:         cfg.BarkToken,
		ServerChanKey:     cfg.ServerChanKey,
		ServerChan3APIURL: cfg.ServerChan3APIURL,
		TelegramBotToken:  cfg.TelegramBotToken,
		TelegramChatID:    cfg.TelegramChatID,
	}
	if ch := nc.EnabledChannels(); len(ch) == 0 {
		log.Println("⚠️ 未配置任何通知渠道（env 或 WebUI worker 设置）")
	} else {
		log.Printf("ℹ️ 通知渠道: %v", ch)
	}
	if cfg.TimeStartRaw == "" {
		log.Println("⚠️ 未设置 TICKET_TIME_START，将不会使用定时抢票")
	} else {
		loc, _ := time.LoadLocation("Asia/Shanghai")
		t, err := time.ParseInLocation("2006-01-02T15:04", cfg.TimeStartRaw, loc)
		if err != nil {
			_ = fmt.Errorf("时间格式错误: %v，正确格式应为 2006-01-02T15:04（北京时间）", err)
		} else {
			cfg.TimeStart = &t
		}
	}
	log.Printf("ℹ️ 抢票重试间隔: %d 毫秒", cfg.Interval)
	if cfg.ProxyList != "" {
		log.Printf("ℹ️ 静态代理列表已配置（长度 %d）", len(cfg.ProxyList))
	}
	if cfg.ProxyAPIURL != "" {
		log.Printf("ℹ️ 代理 API: %s", cfg.ProxyAPIURL)
	}
	log.Printf("ℹ️ 连接池 ConnPerHost=%d CreateBatchSize=%d Warmup=%v",
		cfg.ConnPerHost, cfg.CreateBatchSize, cfg.EnableWarmup)
	return cfg
}

func normalizeConfig(cfg *Config) {
	if cfg.Interval <= 0 {
		cfg.Interval = 300
	}
	if cfg.RateLimitDelayMs <= 0 {
		cfg.RateLimitDelayMs = defaultRateLimitMs
	}
	if cfg.RiskLocalRetries <= 0 {
		cfg.RiskLocalRetries = 5
	}
	if cfg.RiskCooldownBaseMs <= 0 {
		cfg.RiskCooldownBaseMs = 1000
	}
	if cfg.RiskCooldownMaxSec <= 0 {
		cfg.RiskCooldownMaxSec = 30
	}
	if cfg.ProxyMaxFails <= 0 {
		cfg.ProxyMaxFails = 3
	}
	if cfg.ProxyCooldownSec <= 0 {
		cfg.ProxyCooldownSec = 30
	}
	if cfg.ProxyMaxBackoffSec <= 0 {
		cfg.ProxyMaxBackoffSec = 300
	}
	if cfg.ProxyAPICount <= 0 {
		cfg.ProxyAPICount = 5
	}
	if cfg.ProxyAPIScheme == "" {
		cfg.ProxyAPIScheme = "http"
	}
	if cfg.ConnPerHost <= 0 {
		cfg.ConnPerHost = defaultConnPerHost
	}
	if cfg.ConnPerHost > 32 {
		cfg.ConnPerHost = 32
	}
	if cfg.CreateBatchSize <= 0 {
		cfg.CreateBatchSize = 2
	}
	if cfg.CreateBatchSize > cfg.ConnPerHost {
		cfg.CreateBatchSize = cfg.ConnPerHost
	}
}

func SetConfig(cfg *Config) {
	cfgAtomic.Store(cfg)
	Cfg = cfg
}

func GetConfig() *Config {
	if c := cfgAtomic.Load(); c != nil {
		return c
	}
	return Cfg
}
