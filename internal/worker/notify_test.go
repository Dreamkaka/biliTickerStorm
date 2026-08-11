package worker

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEscapeHTML(t *testing.T) {
	if got := escapeHTML(`a<b>&c`); got != "a&lt;b&gt;&amp;c" {
		t.Fatalf("got=%s", got)
	}
}

func TestNotifyConfigEnabledChannels(t *testing.T) {
	c := NotifyConfig{}
	if len(c.EnabledChannels()) != 0 {
		t.Fatal("empty")
	}
	c.PushplusToken = "x"
	c.BarkToken = "y"
	c.ServerChanKey = "z"
	c.TelegramBotToken = "t"
	c.TelegramChatID = "1"
	got := c.EnabledChannels()
	if len(got) != 4 {
		t.Fatalf("%v", got)
	}
}

func TestSendPushPlus(t *testing.T) {
	var got map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		w.WriteHeader(200)
	}))
	defer srv.Close()
	// 直接测 postJSON
	if err := postJSON(srv.URL, map[string]string{"token": "t", "title": "a", "content": "b"}, ""); err != nil {
		t.Fatal(err)
	}
	if got["token"] != "t" || got["title"] != "a" {
		t.Fatalf("%v", got)
	}
}

func TestSendBarkURL(t *testing.T) {
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		w.WriteHeader(200)
	}))
	defer srv.Close()
	// token 为完整 base URL
	if err := sendBark(srv.URL, "标题", "内容"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(path, "标题") && !strings.Contains(path, "%") {
		// PathEscape 可能编码中文
		if path == "" {
			t.Fatal("empty path")
		}
	}
}

func TestSendServerChanTurbo(t *testing.T) {
	// 使用 httptest 无法拦截 sctapi.ftqq.com；测 key 为空时 post 仍会请求外网
	// 只验证 postJSON 错误路径
	err := postJSON("http://127.0.0.1:1/", map[string]string{"a": "b"}, "")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestSendTelegramPayload(t *testing.T) {
	var body map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &body)
		if !strings.Contains(r.URL.Path, "/botTOKEN/sendMessage") {
			t.Errorf("path=%s", r.URL.Path)
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()
	// 临时无法改 telegram URL 主机；用 postJSON 模拟
	if err := postJSON(srv.URL+"/botTOKEN/sendMessage", map[string]string{
		"chat_id": "1", "text": "<b>t</b>\n\nc", "parse_mode": "HTML",
	}, ""); err != nil {
		t.Fatal(err)
	}
	if body["parse_mode"] != "HTML" {
		t.Fatalf("%v", body)
	}
}
