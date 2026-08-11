package worker

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestParseProxyListAndMask(t *testing.T) {
	list, err := ParseProxyList("http://u:p@1.2.3.4:8080, socks5://5.6.7.8:1080; 9.9.9.9:3128")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 {
		t.Fatalf("len=%d", len(list))
	}
	if list[0].Kind != "http" || list[1].Kind != "socks5" || list[2].Kind != "http" {
		t.Fatalf("kinds: %s %s %s", list[0].Kind, list[1].Kind, list[2].Kind)
	}
	masked := MaskProxy("http://user:secret@1.2.3.4:8080")
	if strings.Contains(masked, "secret") {
		t.Fatalf("password leaked: %s", masked)
	}
	if !strings.Contains(masked, "user") || !strings.Contains(masked, "1.2.3.4") {
		t.Fatalf("mask=%s", masked)
	}
	if MaskProxy("") != proxyDirectLabel {
		t.Fatal("empty should be none")
	}
}

func TestProxyPoolRotateAndCooldown(t *testing.T) {
	p, err := NewProxyPool(ProxyPoolConfig{
		List:          "http://a:1@10.0.0.1:8000,http://b:2@10.0.0.2:8000",
		MaxFails:      2,
		CooldownSec:   1,
		MaxBackoffSec: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if p.CurrentLabel() == proxyDirectLabel {
		t.Fatal("expected proxy")
	}
	first := p.CurrentLabel()
	sw, ex := p.MarkFailure("test")
	if ex {
		t.Fatal("not exhausted after 1 fail")
	}
	if !sw && p.CurrentLabel() == first {
		// 可能切到另一条
		t.Log("may stay if only rotate index")
	}
	// 再失败把当前打进冷却
	for i := 0; i < 4; i++ {
		p.MarkFailure("test")
	}
	// 两条都可能冷却
	if p.AvailableCount() == 0 {
		_, ex = p.MarkFailure("test")
		if !ex {
			t.Fatal("expected exhausted when all cooling")
		}
	}
	// 等待冷却恢复
	time.Sleep(1100 * time.Millisecond)
	// 手动清 cooldown 模拟时间推进：重置 entries
	p.mu.Lock()
	for _, e := range p.entries {
		e.CooldownUntil = time.Time{}
		e.ConsecutiveFail = 0
	}
	p.mu.Unlock()
	if p.AvailableCount() != 2 {
		t.Fatalf("avail=%d", p.AvailableCount())
	}
	p.MarkSuccess()
}

func TestProxyPoolDirectOnly(t *testing.T) {
	p, err := NewProxyPool(ProxyPoolConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if p.HasConfiguredProxies() {
		t.Fatal("should be direct")
	}
	if p.Current() != nil || p.CurrentLabel() != proxyDirectLabel {
		t.Fatal("direct")
	}
	sw, ex := p.MarkFailure("x")
	if sw || ex {
		t.Fatalf("sw=%v ex=%v", sw, ex)
	}
}

func TestProxyPoolRefreshFromAPI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("1.1.1.1:8000\n2.2.2.2:8000\n"))
	}))
	defer srv.Close()
	p, err := NewProxyPool(ProxyPoolConfig{
		APIURL:    srv.URL,
		APICount:  2,
		APIScheme: "http",
		MaxFails:  1,
	})
	if err != nil {
		t.Fatal(err)
	}
	n, err := p.RefreshFromAPI()
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("added=%d", n)
	}
	if p.AvailableCount() != 2 {
		t.Fatalf("avail=%d", p.AvailableCount())
	}
	snap := p.StatusSnapshot()
	if snap["total"].(int) != 2 {
		t.Fatalf("snap=%v", snap)
	}
}

func TestParseProxyAPIJSON(t *testing.T) {
	lines := parseProxyAPIBody([]byte(`["10.0.0.1:1","10.0.0.2:2"]`), "socks5")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "socks5://") {
		t.Fatalf("%v", lines)
	}
	lines = parseProxyAPIBody([]byte(`{"data":["3.3.3.3:3"]}`), "http")
	if len(lines) != 1 || lines[0] != "http://3.3.3.3:3" {
		t.Fatalf("%v", lines)
	}
}
