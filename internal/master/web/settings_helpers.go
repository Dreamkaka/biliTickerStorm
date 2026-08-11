package web

import (
	"biliTickerStorm/internal/common/workercfg"
	"strings"
)

func decodeWorkerSettingsJSON(raw []byte) (workercfg.Settings, error) {
	return workercfg.FromJSON(raw)
}

// mergePreserveSecrets：表单回传脱敏值（含 ****）时保留磁盘原密钥；空字符串表示清空。
func mergePreserveSecrets(s *Server, incoming workercfg.Settings) workercfg.Settings {
	full, _ := s.master.GetWorkerSettingsFull()
	keepSecret := func(in, old string) string {
		if strings.Contains(in, "****") || strings.Contains(in, "***") {
			return old
		}
		return strings.TrimSpace(in)
	}
	incoming.PushplusToken = keepSecret(incoming.PushplusToken, full.PushplusToken)
	incoming.BarkToken = keepSecret(incoming.BarkToken, full.BarkToken)
	incoming.ServerChanKey = keepSecret(incoming.ServerChanKey, full.ServerChanKey)
	incoming.ServerChan3APIURL = keepSecret(incoming.ServerChan3APIURL, full.ServerChan3APIURL)
	incoming.TelegramBotToken = keepSecret(incoming.TelegramBotToken, full.TelegramBotToken)
	incoming.TelegramHTTPProxy = keepSecret(incoming.TelegramHTTPProxy, full.TelegramHTTPProxy)
	if strings.Contains(incoming.ProxyList, "***") {
		incoming.ProxyList = full.ProxyList
	}
	incoming.Normalize()
	return incoming
}
