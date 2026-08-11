package worker

import (
	"fmt"
	"math/rand"
	"regexp"
	"strings"
)

// BrowserFingerprint 对齐 biliTickerBuy BrowerState：同一会话复用，不随每次请求重生成。
type BrowserFingerprint struct {
	UserAgent       string
	Platform        string // Win32 / MacIntel / Linux x86_64
	Languages       []string
	SecChUA         string
	SecChUAPlatform string
	AcceptLanguage  string
}

var chromeMajorRe = regexp.MustCompile(`Chrome/(\d+)`)

// NewBrowserFingerprint 生成桌面 Chrome 指纹（默认 Windows + zh-CN）。
func NewBrowserFingerprint() BrowserFingerprint {
	osName := "windows"
	ua := buildChromeUserAgent(osName)
	platform := "Win32"
	secPlatform := `"Windows"`
	langs := []string{"zh-CN", "zh", "en-US", "en"}
	// 随机语言列表变体
	switch rand.Intn(3) {
	case 0:
		langs = []string{"zh-CN", "zh"}
	case 1:
		langs = []string{"zh-CN", "zh", "en"}
	}
	return BrowserFingerprint{
		UserAgent:       ua,
		Platform:        platform,
		Languages:       langs,
		SecChUA:         buildSecChUA(ua),
		SecChUAPlatform: secPlatform,
		AcceptLanguage:  buildAcceptLanguage(langs),
	}
}

func buildChromeUserAgent(osName string) string {
	majors := []int{126, 127, 128, 129, 130, 131, 132, 133}
	major := majors[rand.Intn(len(majors))]
	build := 6000 + rand.Intn(900)
	patch := 80 + rand.Intn(100)
	ver := fmt.Sprintf("%d.0.%d.%d", major, build, patch)
	system := "Windows NT 10.0; Win64; x64"
	if osName == "macos" {
		system = "Macintosh; Intel Mac OS X 10_15_7"
	} else if osName == "linux" {
		system = "X11; Linux x86_64"
	}
	return fmt.Sprintf(
		"Mozilla/5.0 (%s) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/%s Safari/537.36",
		system, ver,
	)
}

func buildSecChUA(userAgent string) string {
	major := "131"
	if m := chromeMajorRe.FindStringSubmatch(userAgent); len(m) == 2 {
		major = m[1]
	}
	return fmt.Sprintf(`"Google Chrome";v="%s", "Chromium";v="%s", "Not_A Brand";v="24"`, major, major)
}

func buildAcceptLanguage(langs []string) string {
	if len(langs) == 0 {
		return "zh-CN,zh;q=0.9,en;q=0.8"
	}
	parts := make([]string, 0, len(langs))
	for i, lang := range langs {
		if i == 0 {
			parts = append(parts, lang)
			continue
		}
		q := 1.0 - float64(i)*0.1
		if q < 0.1 {
			q = 0.1
		}
		parts = append(parts, fmt.Sprintf("%s;q=%.1f", lang, q))
	}
	return strings.Join(parts, ",")
}

// ApplyJSONHeaders 写入类浏览器 JSON 请求头（对齐 build_headers_from_browser_state）。
func (fp *BrowserFingerprint) ApplyJSONHeaders(set func(k, v string), contentType, referer string) {
	if contentType == "" {
		contentType = "application/json"
	}
	if referer == "" {
		referer = "https://show.bilibili.com/"
	}
	if !strings.HasSuffix(referer, "/") {
		referer += "/"
	}
	// 顺序尽量贴近 Chrome fetch
	set("Accept", "application/json, text/plain, */*")
	set("Accept-Language", fp.AcceptLanguage)
	set("Accept-Encoding", "gzip, deflate")
	set("Content-Type", contentType)
	set("Origin", "https://show.bilibili.com")
	set("Referer", referer)
	set("Sec-Ch-Ua", fp.SecChUA)
	set("Sec-Ch-Ua-Mobile", "?0")
	set("Sec-Ch-Ua-Platform", fp.SecChUAPlatform)
	set("Sec-Fetch-Dest", "empty")
	set("Sec-Fetch-Mode", "cors")
	set("Sec-Fetch-Site", "same-origin")
	set("Priority", "u=1, i")
	set("User-Agent", fp.UserAgent)
}

// UserAgentLength 供 ctoken href/ua 长度一致。
func (fp *BrowserFingerprint) UserAgentLength() int {
	if fp == nil || fp.UserAgent == "" {
		return len(defaultUserAgent)
	}
	return len(fp.UserAgent)
}
