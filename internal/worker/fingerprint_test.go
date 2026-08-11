package worker

import (
	"strings"
	"testing"
)

func TestNewBrowserFingerprintChromeLike(t *testing.T) {
	fp := NewBrowserFingerprint()
	if !strings.Contains(fp.UserAgent, "Chrome/") {
		t.Fatalf("ua=%s", fp.UserAgent)
	}
	if !strings.Contains(fp.UserAgent, "Mozilla/5.0") {
		t.Fatalf("ua missing mozilla: %s", fp.UserAgent)
	}
	if fp.SecChUA == "" || !strings.Contains(fp.SecChUA, "Chromium") {
		t.Fatalf("sec-ch-ua=%s", fp.SecChUA)
	}
	if fp.SecChUAPlatform != `"Windows"` {
		t.Fatalf("platform=%s", fp.SecChUAPlatform)
	}
	if !strings.Contains(fp.AcceptLanguage, "zh-CN") {
		t.Fatalf("al=%s", fp.AcceptLanguage)
	}
	if fp.UserAgentLength() != len(fp.UserAgent) {
		t.Fatal("ua length mismatch")
	}
}

func TestApplyJSONHeaders(t *testing.T) {
	fp := NewBrowserFingerprint()
	h := map[string]string{}
	fp.ApplyJSONHeaders(func(k, v string) { h[k] = v }, "application/json", "https://show.bilibili.com")
	for _, k := range []string{
		"User-Agent", "Accept", "Accept-Language", "Origin", "Referer",
		"Sec-Ch-Ua", "Sec-Ch-Ua-Mobile", "Sec-Ch-Ua-Platform",
		"Sec-Fetch-Dest", "Sec-Fetch-Mode", "Sec-Fetch-Site", "Content-Type",
	} {
		if h[k] == "" {
			t.Fatalf("missing header %s", k)
		}
	}
	if h["Referer"] != "https://show.bilibili.com/" {
		t.Fatalf("referer=%s", h["Referer"])
	}
	if h["Sec-Fetch-Mode"] != "cors" {
		t.Fatalf("mode=%s", h["Sec-Fetch-Mode"])
	}
}

func TestBuildSecChUAMajor(t *testing.T) {
	ua := "Mozilla/5.0 (...) Chrome/129.0.6500.1 Safari/537.36"
	got := buildSecChUA(ua)
	if !strings.Contains(got, `v="129"`) {
		t.Fatalf("got=%s", got)
	}
}
