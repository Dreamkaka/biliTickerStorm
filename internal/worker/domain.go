package worker

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	baseURL            = "https://show.bilibili.com"
	requestSource      = "neul-next"
	defaultCreateRetry = 60
	defaultRateLimitMs = 300
	// defaultUserAgent 仅作测试/回退；线上请求使用 BrowserFingerprint
	defaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
	errnoDuplicateBuy  = 100003
	errnoStockEmpty    = 100001
	errnoPayMoney      = 100034
	errnoCaptcha       = 100044
	errnoPendingOrder  = 100048
	errnoTokenExpired  = 100051
	errnoDupOrder      = 100079
)

func projectDetailURL(projectID int) string {
	return fmt.Sprintf("%s/api/ticket/project/getV2?id=%d", baseURL, projectID)
}

func mobileDetailHref(projectID int) string {
	return fmt.Sprintf("https://mall.bilibili.com/neul-next/ticket-renovation/detail.html?id=%d", projectID)
}

type BiliTickerBuyConfig struct {
	Username     string      `json:"username"`
	Detail       string      `json:"detail"`
	Count        int         `json:"count"`
	ScreenId     int         `json:"screen_id"`
	ProjectId    int         `json:"project_id"`
	SkuId        int         `json:"sku_id"`
	OrderType    int         `json:"order_type"`
	PayMoney     int         `json:"pay_money"`
	BuyerInfo    []BuyerInfo `json:"buyer_info"`
	Buyer        string      `json:"buyer"`
	Tel          string      `json:"tel"`
	DeliverInfo  DeliverInfo `json:"deliver_info"`
	Cookies      []Cookies   `json:"cookies"`
	Phone        string      `json:"phone"`
	IsHotProject bool        `json:"is_hot_project"`
	// SaleStart 可能是字符串或 unix 数字；用 FlexibleTimeString 兼容 Buy 导出
	SaleStart FlexibleTimeString `json:"sale_start"`
	LinkId    interface{}        `json:"link_id,omitempty"`
	Token        string      `json:"token"`
	Again        int         `json:"again"`
	Timestamp    int64       `json:"timestamp"`
}
type Cookies struct {
	Name     string  `json:"name"`
	Value    string  `json:"value"`
	Domain   string  `json:"domain"`
	Path     string  `json:"path"`
	Expires  float64 `json:"expires"`
	HttpOnly bool    `json:"httpOnly"`
	Secure   bool    `json:"secure"`
	SameSite string  `json:"sameSite"`
}
type DeliverInfo struct {
	Name   string `json:"name"`
	Tel    string `json:"tel"`
	AddrId int    `json:"addr_id"`
	Addr   string `json:"addr"`
}
type BuyerInfo struct {
	Id             int    `json:"id"`
	Uid            int    `json:"uid"`
	AccountChannel string `json:"account_channel"`
	PersonalId     string `json:"personal_id"`
	Name           string `json:"name"`
	IdCardFront    string `json:"id_card_front"`
	IdCardBack     string `json:"id_card_back"`
	IsDefault      int    `json:"is_default"`
	Tel            string `json:"tel"`
	ErrorCode      string `json:"error_code"`
	IdType         int    `json:"id_type"`
	VerifyStatus   int    `json:"verify_status"`
	AccountId      int    `json:"accountId"`
}

// 对齐 biliTickerBuy util/ErrorCodes.py
var errnoDict = map[int]string{
	0:      "成功",
	3:      "下单过于频繁，请稍后再试",
	100001: "暂无可售票或登录状态异常",
	100003: "重复购买",
	100009: "库存不足",
	100016: "项目不可售",
	100017: "票种不可售",
	100034: "票价错误",
	100039: "活动收摊啦,下次要快点哦",
	100041: "未到开票时间",
	100044: "需要完成人机验证",
	100048: "已经下单，有尚未完成订单",
	100051: "订单准备过期，重新验证",
	219:    "下单失败，请重试",
	221:    "下单请求过多，请稍后再试",
	900001: "下单过快，被系统限制",
	900002: "当前请求较多，请稍后再试",
}

type CreateV2RequestBody struct {
	Count          int    `json:"count"`
	ScreenId       int    `json:"screen_id"`
	ProjectId      int    `json:"project_id"`
	SkuId          int    `json:"sku_id"`
	OrderType      int    `json:"order_type"`
	PayMoney       int    `json:"pay_money"`
	BuyerInfo      string `json:"buyer_info"`
	Buyer          string `json:"buyer"`
	Tel            string `json:"tel"`
	DeliverInfo    string `json:"deliver_info"`
	Again          int    `json:"again"`
	Token          string `json:"token"`
	Timestamp      int64  `json:"timestamp"`
	CToken         string `json:"ctoken"`
	PToken         string `json:"ptoken"`
	NewRisk        bool   `json:"newRisk"`
	RequestSource  string `json:"requestSource"`
	OrderCreateUrl string `json:"orderCreateUrl"`
}

func (cfg *BiliTickerBuyConfig) orderTypeOrDefault() int {
	if cfg.OrderType == 0 {
		return 1
	}
	return cfg.OrderType
}

func (cfg *BiliTickerBuyConfig) BuildPreparePayload(prepareCToken string) map[string]interface{} {
	return map[string]interface{}{
		"count":              cfg.Count,
		"screen_id":          cfg.ScreenId,
		"order_type":         cfg.orderTypeOrDefault(),
		"project_id":         cfg.ProjectId,
		"sku_id":             cfg.SkuId,
		"buyer_info":         cfg.BuyerInfo,
		"ignoreRequestLimit": true,
		"ticket_agent":       "",
		"token":              prepareCToken,
		"newRisk":            true,
		"requestSource":      requestSource,
	}
}

func (cfg *BiliTickerBuyConfig) ToCreateV2RequestBody(orderToken, ctoken, ptoken string, nowMs int64) (*CreateV2RequestBody, error) {
	buyerInfoStr, err := json.Marshal(cfg.BuyerInfo)
	if err != nil {
		return nil, fmt.Errorf("marshal buyer_info: %w", err)
	}
	deliverInfoStr, err := json.Marshal(cfg.DeliverInfo)
	if err != nil {
		return nil, fmt.Errorf("marshal deliver_info: %w", err)
	}
	return &CreateV2RequestBody{
		Count:          cfg.Count,
		ScreenId:       cfg.ScreenId,
		ProjectId:      cfg.ProjectId,
		SkuId:          cfg.SkuId,
		OrderType:      cfg.orderTypeOrDefault(),
		PayMoney:       cfg.PayMoney,
		BuyerInfo:      string(buyerInfoStr),
		Buyer:          cfg.Buyer,
		Tel:            cfg.Tel,
		DeliverInfo:    string(deliverInfoStr),
		Again:          1,
		Token:          orderToken,
		Timestamp:      nowMs,
		CToken:         ctoken,
		PToken:         ptoken,
		NewRisk:        true,
		RequestSource:  requestSource,
		OrderCreateUrl: baseURL + "/api/ticket/order/createV2",
	}, nil
}

func isCreateSuccess(ret map[string]interface{}, errno int) bool {
	if errno != 0 {
		return false
	}
	msg := getStringFromMap(ret, "msg", "message")
	return !strings.Contains(msg, "defaultBBR")
}

func errnoMessage(errno int) string {
	if msg, ok := errnoDict[errno]; ok {
		return msg
	}
	return "未知错误码"
}

func extractPrepareToken(result map[string]interface{}) string {
	data, ok := result["data"].(map[string]interface{})
	if !ok {
		return ""
	}
	token, ok := data["token"].(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(token)
}

func extractPreparePToken(result map[string]interface{}) string {
	data, ok := result["data"].(map[string]interface{})
	if !ok {
		return ""
	}
	return normalizePreparePToken(data["ptoken"])
}

func getStringFromMap(m map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if val, ok := m[key]; ok {
			if s, ok := val.(string); ok {
				return s
			}
		}
	}
	return ""
}
