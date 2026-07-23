package worker

import (
	"encoding/base64"
	"testing"
)

func TestGenerateCTokenFixed(t *testing.T) {
	// 固定参数，校验字节布局与 base64 可解码
	token := generateCToken(
		1, 2, 3, 4, 5, 6, 7, 8, 9, 10,
		11, 12, 13, 14, 15,
	)
	raw, err := base64.StdEncoding.DecodeString(token)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	// 前 16 字节：m1,0,touch,0,m2,0,vis,0,m3,0,m4,0,beforeunload,0,m5,0
	wantPrefix := []byte{1, 0, 2, 0, 3, 0, 4, 0, 5, 0, 6, 0, 15, 0, 8, 0}
	if len(raw) < len(wantPrefix) {
		t.Fatalf("raw too short: %d", len(raw))
	}
	for i := range wantPrefix {
		if raw[i] != wantPrefix[i] {
			t.Fatalf("byte %d: got %d want %d (raw=%v)", i, raw[i], wantPrefix[i], raw)
		}
	}
}

func TestIsCreateSuccessRejectsDefaultBBR(t *testing.T) {
	ret := map[string]interface{}{"errno": 0, "msg": "defaultBBR something"}
	if isCreateSuccess(ret, 0) {
		t.Fatal("should reject defaultBBR")
	}
	ret2 := map[string]interface{}{"errno": 0, "msg": "ok"}
	if !isCreateSuccess(ret2, 0) {
		t.Fatal("should accept errno 0 without defaultBBR")
	}
}

func TestNormalizePreparePToken(t *testing.T) {
	if got := normalizePreparePToken("abc=def="); got != "abcdef" {
		t.Fatalf("got %q", got)
	}
}

func TestToCreateV2IncludesToken(t *testing.T) {
	cfg := BiliTickerBuyConfig{
		Count: 1, ScreenId: 2, ProjectId: 3, SkuId: 4, OrderType: 1, PayMoney: 100,
		BuyerInfo: []BuyerInfo{{Id: 1, Name: "a"}},
		Buyer:     "a", Tel: "1",
	}
	body, err := cfg.ToCreateV2RequestBody("ORDER_TOKEN", "CT", "PT", 12345)
	if err != nil {
		t.Fatal(err)
	}
	if body.Token != "ORDER_TOKEN" {
		t.Fatalf("token not set: %q", body.Token)
	}
	if body.CToken != "CT" || body.PToken != "PT" {
		t.Fatalf("ctoken/ptoken missing: %+v", body)
	}
	if body.RequestSource != requestSource || !body.NewRisk {
		t.Fatalf("request flags: %+v", body)
	}
}
