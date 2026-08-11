package ticketcfg

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildFromSelection_ExactBuyShape(t *testing.T) {
	cfg, name, err := BuildFromSelection(SelectionInput{
		Username:    "幻梦晓寒kaka",
		ProjectName: "东莞·紫微游戏动漫展",
		ProjectID:   1001763,
		Phone:       "",
		Ticket: map[string]interface{}{
			"id":             float64(882645),
			"screen_id":      float64(1007028),
			"project_id":     float64(1001763),
			"price":          float64(7500),
			"display":        "2026年7月25日 - 预售普通票 - ￥75 - 预售 - 【起售时间：2026-05-16 00:00:00】",
			"sale_start":     "2026-05-16 00:00:00",
			"is_hot_project": false,
		},
		Buyers: []map[string]interface{}{
			{
				"id": float64(14118166), "uid": float64(1657491474), "account_channel": "",
				"personal_id": "441************890", "name": "陈钰舟",
				"id_card_front": "", "id_card_back": "", "is_default": float64(1),
				"tel": "phone", "error_code": "0", "id_type": float64(0),
				"verify_status": float64(1), "accountId": float64(1657491474),
			},
		},
		Address: map[string]interface{}{
			"name": "name", "phone": "1111", "id": float64(34586819),
			"prov": "", "city": "", "area": "", "addr": "地址",
		},
		BuyerName: "1111",
		BuyerTel:  "111",
		Cookies: []Cookie{
			{Name: "SESSDATA", Value: "abc", Domain: ".bilibili.com", Path: "/", HttpOnly: true},
			{Name: "bili_jct", Value: "def", Domain: "bilibili.com"},
			{Name: "DedeUserID", Value: "1657491474"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	raw, err := MarshalConfigFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)

	// cookies 只有 name/value
	if strings.Contains(text, `"domain"`) || strings.Contains(text, `"httpOnly"`) || strings.Contains(text, `"path"`) {
		t.Fatalf("cookies must be name/value only:\n%s", text)
	}
	// 顶层键顺序：按「行首缩进 + 键」匹配，避免嵌套 buyer_info.tel 干扰
	topKeys := []string{
		"username", "detail", "count", "screen_id", "project_id",
		"is_hot_project", "sku_id", "sale_start", "order_type", "pay_money",
		"buyer_info", "buyer", "tel", "deliver_info", "cookies", "phone",
	}
	prev := -1
	for _, k := range topKeys {
		needle := "\n    \"" + k + "\""
		i := strings.Index(text, needle)
		if i < 0 {
			// 首行 username
			if k == "username" {
				i = strings.Index(text, "    \""+k+"\"")
			}
		}
		if i < 0 || i < prev {
			t.Fatalf("top key order broken at %s (i=%d prev=%d)\n%s", k, i, prev, text)
		}
		prev = i
	}
	// buyer_info 内顺序
	start := strings.Index(text, `"buyer_info"`)
	end := strings.Index(text[start:], `"buyer"`)
	chunk := text[start : start+end]
	buyerKeys := []string{"id", "uid", "account_channel", "personal_id", "name",
		"id_card_front", "id_card_back", "is_default", "tel", "error_code",
		"id_type", "verify_status", "accountId"}
	prev = -1
	for _, k := range buyerKeys {
		needle := "\"" + k + "\""
		i := strings.Index(chunk, needle)
		if i < 0 || i < prev {
			t.Fatalf("buyer key order broken at %s\n%s", k, chunk)
		}
		prev = i
	}
	if cfg.SaleStart != "2026-05-16 00:00:00" {
		t.Fatal(cfg.SaleStart)
	}
	if cfg.Count != 1 || cfg.PayMoney != 7500 {
		t.Fatalf("count/pay %d %d", cfg.Count, cfg.PayMoney)
	}
	if !strings.Contains(cfg.Detail, "陈钰舟") || !strings.Contains(cfg.Detail, "东莞") {
		t.Fatal(cfg.Detail)
	}
	if !strings.Contains(name, "幻梦晓寒kaka") {
		t.Fatal(name)
	}
	// indent 4
	if !strings.Contains(text, "\n    \"username\"") {
		t.Fatalf("indent:\n%s", text)
	}
	// 可反序列化
	var round map[string]interface{}
	if err := json.Unmarshal(raw, &round); err != nil {
		t.Fatal(err)
	}
	cookies := round["cookies"].([]interface{})
	first := cookies[0].(map[string]interface{})
	if len(first) != 2 {
		t.Fatalf("cookie keys: %v", first)
	}
}

func TestFilenameFilter(t *testing.T) {
	if strings.ContainsAny(FilenameFilter(`a/b:c`), `\/:`) {
		t.Fatal()
	}
	if !strings.Contains(FilenameFilter("中文"), "中文") {
		t.Fatal()
	}
}
