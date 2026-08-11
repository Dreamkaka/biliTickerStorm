package worker

import (
	"encoding/json"
	"strings"
	"testing"
)

func sampleConfig() BiliTickerBuyConfig {
	return BiliTickerBuyConfig{
		Count:     2,
		ScreenId:  100,
		ProjectId: 200,
		SkuId:     300,
		OrderType: 1,
		PayMoney:  12800,
		BuyerInfo: []BuyerInfo{{Id: 1, Name: "测试", Tel: "13800138000"}},
		Buyer:     "测试",
		Tel:       "13800138000",
		DeliverInfo: DeliverInfo{
			Name: "测试", Tel: "13800138000", AddrId: 1, Addr: "addr",
		},
	}
}

func TestBuildPreparePayloadFields(t *testing.T) {
	cfg := sampleConfig()
	p := cfg.BuildPreparePayload("PREPARE_CTOKEN")
	wantKeys := []string{
		"count", "screen_id", "order_type", "project_id", "sku_id",
		"buyer_info", "ignoreRequestLimit", "ticket_agent", "token",
		"newRisk", "requestSource",
	}
	for _, k := range wantKeys {
		if _, ok := p[k]; !ok {
			t.Fatalf("missing key %s", k)
		}
	}
	if p["token"] != "PREPARE_CTOKEN" {
		t.Fatalf("token=%v", p["token"])
	}
	if p["ignoreRequestLimit"] != true || p["newRisk"] != true {
		t.Fatalf("flags: %+v", p)
	}
	if p["requestSource"] != requestSource {
		t.Fatalf("requestSource=%v", p["requestSource"])
	}
	if p["ticket_agent"] != "" {
		t.Fatalf("ticket_agent=%v", p["ticket_agent"])
	}
	if p["count"] != 2 || p["screen_id"] != 100 || p["project_id"] != 200 || p["sku_id"] != 300 {
		t.Fatalf("ids: %+v", p)
	}
	// 不应包含 create 专用字段
	for _, bad := range []string{"detail", "sale_start", "username", "pay_money", "again", "ctoken", "ptoken"} {
		if _, ok := p[bad]; ok {
			t.Fatalf("prepare payload should not contain %s", bad)
		}
	}
}

func TestBuildPreparePayloadDefaultOrderType(t *testing.T) {
	cfg := sampleConfig()
	cfg.OrderType = 0
	p := cfg.BuildPreparePayload("x")
	if p["order_type"] != 1 {
		t.Fatalf("default order_type=%v", p["order_type"])
	}
}

func TestToCreateV2RequestBodyFull(t *testing.T) {
	cfg := sampleConfig()
	body, err := cfg.ToCreateV2RequestBody("ORDER", "CT", "PT", 999)
	if err != nil {
		t.Fatal(err)
	}
	if body.Again != 1 || body.Timestamp != 999 {
		t.Fatalf("again/ts: %+v", body)
	}
	if body.Token != "ORDER" || body.CToken != "CT" || body.PToken != "PT" {
		t.Fatalf("tokens: %+v", body)
	}
	if !body.NewRisk || body.RequestSource != requestSource {
		t.Fatalf("flags: %+v", body)
	}
	if body.OrderCreateUrl != baseURL+"/api/ticket/order/createV2" {
		t.Fatalf("url=%s", body.OrderCreateUrl)
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"detail", "sale_start", "username"} {
		if _, ok := m[bad]; ok {
			t.Fatalf("create body should not contain %s", bad)
		}
	}
}

func TestExtractPrepareTokenAndPToken(t *testing.T) {
	ret := map[string]interface{}{
		"data": map[string]interface{}{
			"token":  "  ord-token  ",
			"ptoken": "ab=cd=",
		},
	}
	if got := extractPrepareToken(ret); got != "ord-token" {
		t.Fatalf("token=%q", got)
	}
	if got := extractPreparePToken(ret); got != "abcd" {
		t.Fatalf("ptoken=%q", got)
	}
	if extractPrepareToken(map[string]interface{}{}) != "" {
		t.Fatal("empty should yield empty token")
	}
}

func TestMobileDetailHrefLength(t *testing.T) {
	href := mobileDetailHref(12345)
	want := "https://mall.bilibili.com/neul-next/ticket-renovation/detail.html?id=12345"
	if href != want {
		t.Fatalf("href=%s", href)
	}
	if len(defaultUserAgent) == 0 {
		t.Fatal("UA empty")
	}
}

func TestClassifyCreateErrno(t *testing.T) {
	ok := map[string]interface{}{"errno": 0, "msg": "ok"}
	if classifyCreateErrno(0, ok) != CreateSuccess {
		t.Fatal("success")
	}
	bbr := map[string]interface{}{"errno": 0, "msg": "defaultBBR"}
	if classifyCreateErrno(0, bbr) != CreateRetry {
		t.Fatal("defaultBBR should retry")
	}
	cases := []struct {
		errno int
		want  CreateAction
	}{
		{100003, CreateTerminal},
		{100048, CreateTerminal},
		{100079, CreateTerminal},
		{100051, CreateTokenExpired},
		{100034, CreateUpdatePayMoney},
		{100001, CreateProjectRefresh},
		{-401, CreateCaptchaRisk},
		{100044, CreateCaptchaRisk},
		{3, CreateRetry},
	}
	for _, c := range cases {
		if got := classifyCreateErrno(c.errno, map[string]interface{}{}); got != c.want {
			t.Fatalf("errno %d: got %v want %v", c.errno, got, c.want)
		}
	}
}

func TestExtractPayMoneyAndOrderURL(t *testing.T) {
	ret := map[string]interface{}{
		"data": map[string]interface{}{
			"pay_money": float64(25600),
			"orderId":   float64(98765),
		},
	}
	pay, ok := extractPayMoney(ret)
	if !ok || pay != 25600 {
		t.Fatalf("pay=%d ok=%v", pay, ok)
	}
	url := extractOrderURL(ret)
	if url != "https://show.bilibili.com/platform/orderDetail.html?order_id=98765" {
		t.Fatalf("url=%s", url)
	}
}

func TestDiagnoseHTTPError(t *testing.T) {
	s := diagnoseHTTPError(500, "text/html", []byte("<html>err</html>"))
	for _, part := range []string{"HTTP 500", "text/html", "<html>"} {
		if !strings.Contains(s, part) {
			t.Fatalf("diag missing %q: %s", part, s)
		}
	}
}

func TestRiskCooldown(t *testing.T) {
	d1 := riskCooldown(1, 1000, 30)
	d3 := riskCooldown(3, 1000, 30)
	d99 := riskCooldown(99, 1000, 5)
	if d1.Milliseconds() != 1000 {
		t.Fatalf("d1=%v", d1)
	}
	if d3.Milliseconds() != 3000 {
		t.Fatalf("d3=%v", d3)
	}
	if d99.Milliseconds() != 5000 {
		t.Fatalf("d99=%v", d99)
	}
}
