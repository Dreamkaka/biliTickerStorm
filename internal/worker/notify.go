package worker

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// NotifyConfig 对齐 biliTickerBuy NotifierConfig 的常用渠道。
type NotifyConfig struct {
	PushplusToken     string
	BarkToken         string
	ServerChanKey     string // Server酱ᵀᵘʳᵇᵒ SCT key
	ServerChan3APIURL string // Server酱³ 完整推送 URL
	TelegramBotToken  string
	TelegramChatID    string
	TelegramHTTPProxy string
}

func notifyConfigFromEnv() NotifyConfig {
	c := GetConfig()
	if c == nil {
		return NotifyConfig{}
	}
	return NotifyConfig{
		PushplusToken:     c.PushplusToken,
		BarkToken:         c.BarkToken,
		ServerChanKey:     c.ServerChanKey,
		ServerChan3APIURL: c.ServerChan3APIURL,
		TelegramBotToken:  c.TelegramBotToken,
		TelegramChatID:    c.TelegramChatID,
		TelegramHTTPProxy: c.TelegramHTTPProxy,
	}
}

func (c NotifyConfig) EnabledChannels() []string {
	var names []string
	if strings.TrimSpace(c.PushplusToken) != "" {
		names = append(names, "PushPlus")
	}
	if strings.TrimSpace(c.BarkToken) != "" {
		names = append(names, "Bark")
	}
	if strings.TrimSpace(c.ServerChanKey) != "" {
		names = append(names, "ServerChanTurbo")
	}
	if strings.TrimSpace(c.ServerChan3APIURL) != "" {
		names = append(names, "ServerChan3")
	}
	if strings.TrimSpace(c.TelegramBotToken) != "" && strings.TrimSpace(c.TelegramChatID) != "" {
		names = append(names, "Telegram")
	}
	return names
}

// NotifyAll 并发向所有已配置渠道发送；等待最多 timeout。
func NotifyAll(title, content string, timeout time.Duration) {
	cfg := notifyConfigFromEnv()
	channels := cfg.EnabledChannels()
	if len(channels) == 0 {
		return
	}
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	log.Infof("发送通知: channels=%v title=%s", channels, title)

	var wg sync.WaitGroup
	for _, name := range channels {
		name := name
		wg.Add(1)
		go func() {
			defer wg.Done()
			var err error
			switch name {
			case "PushPlus":
				err = sendPushPlus(cfg.PushplusToken, title, content)
			case "Bark":
				err = sendBark(cfg.BarkToken, title, content)
			case "ServerChanTurbo":
				err = sendServerChanTurbo(cfg.ServerChanKey, title, content)
			case "ServerChan3":
				err = sendServerChan3(cfg.ServerChan3APIURL, title, content)
			case "Telegram":
				err = sendTelegram(cfg.TelegramBotToken, cfg.TelegramChatID, cfg.TelegramHTTPProxy, title, content)
			}
			if err != nil {
				log.Warnf("通知 %s 失败: %v", name, err)
			} else {
				log.Infof("通知 %s 已发送", name)
			}
		}()
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(timeout):
		log.Warn("通知发送超时，部分渠道可能未完成")
	}
}

func notifyHTTPClient(proxyURL string) *http.Client {
	tr := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
	}
	if strings.TrimSpace(proxyURL) != "" {
		if u, err := url.Parse(proxyURL); err == nil {
			tr.Proxy = http.ProxyURL(u)
		}
	}
	return &http.Client{Timeout: 12 * time.Second, Transport: tr}
}

func postJSON(rawURL string, payload interface{}, proxyURL string) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, rawURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := notifyHTTPClient(proxyURL).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}

// sendPushPlus 对齐 PushPlus HTTP API。
func sendPushPlus(token, title, content string) error {
	return postJSON("https://www.pushplus.plus/send", map[string]string{
		"token":   token,
		"title":   title,
		"content": content,
	}, "")
}

// sendBark 对齐 BarkUtil：token 可为 key 或完整 base URL。
func sendBark(token, title, content string) error {
	token = strings.TrimSpace(token)
	var endpoint string
	if u, err := url.Parse(token); err == nil && (u.Scheme == "http" || u.Scheme == "https") {
		endpoint = strings.TrimRight(token, "/") + "/" + url.PathEscape(title) + "/" + url.PathEscape(content)
	} else {
		endpoint = fmt.Sprintf("https://api.day.app/%s/%s/%s",
			url.PathEscape(token), url.PathEscape(title), url.PathEscape(content))
	}
	payload := map[string]string{
		"icon":   "https://raw.githubusercontent.com/mikumifa/biliTickerBuy/refs/heads/main/assets/icon.ico",
		"group":  "biliTickerStorm",
		"url":    "https://mall.bilibili.com/neul/index.html?page=box_me&noTitleBar=1",
		"sound":  "telegraph",
		"level":  "critical",
		"volume": "10",
	}
	return postJSON(endpoint, payload, "")
}

// sendServerChanTurbo Server酱ᵀᵘʳᵇᵒ。
func sendServerChanTurbo(key, title, content string) error {
	key = strings.TrimSpace(key)
	endpoint := fmt.Sprintf("https://sctapi.ftqq.com/%s.send", url.PathEscape(key))
	return postJSON(endpoint, map[string]string{
		"title": title,
		"desp":  content,
	}, "")
}

// sendServerChan3 Server酱³ 完整 API URL。
func sendServerChan3(apiURL, title, content string) error {
	apiURL = strings.TrimSpace(apiURL)
	return postJSON(apiURL, map[string]string{
		"title": title,
		"desp":  content,
	}, "")
}

// sendTelegram Bot API sendMessage。
func sendTelegram(botToken, chatID, httpProxy, title, content string) error {
	botToken = strings.TrimSpace(botToken)
	chatID = strings.TrimSpace(chatID)
	endpoint := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", botToken)
	text := fmt.Sprintf("<b>%s</b>\n\n%s", escapeHTML(title), escapeHTML(content))
	return postJSON(endpoint, map[string]string{
		"chat_id":    chatID,
		"text":       text,
		"parse_mode": "HTML",
	}, httpProxy)
}

func escapeHTML(s string) string {
	r := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
	)
	return r.Replace(s)
}

// 兼容旧调用名
func sendPushPlusMessage(token, title, content string) error {
	return sendPushPlus(token, title, content)
}
