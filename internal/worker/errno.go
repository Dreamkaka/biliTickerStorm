package worker

import (
	"fmt"
	"strings"
)

// CreateAction 描述 createV2 业务码对应的后续动作。
type CreateAction int

const (
	CreateRetry CreateAction = iota
	CreateSuccess
	CreateTerminal
	CreateTokenExpired
	CreateUpdatePayMoney
	CreateProjectRefresh
	CreateCaptchaRisk
)

func classifyCreateErrno(errno int, ret map[string]interface{}) CreateAction {
	if isCreateSuccess(ret, errno) {
		return CreateSuccess
	}
	switch errno {
	case 100003, 100048, 100079:
		return CreateTerminal
	case 100051:
		return CreateTokenExpired
	case 100034:
		return CreateUpdatePayMoney
	case 100001:
		return CreateProjectRefresh
	case -401, 100044:
		return CreateCaptchaRisk
	default:
		return CreateRetry
	}
}

// extractOrderID 对齐上游 extract_order_id（优先 orderId）。
func extractOrderID(ret map[string]interface{}) string {
	if ret == nil {
		return ""
	}
	data, ok := ret["data"].(map[string]interface{})
	if !ok {
		return ""
	}
	for _, key := range []string{"orderId", "order_id", "id"} {
		if v, ok := data[key]; ok {
			switch n := v.(type) {
			case string:
				if s := strings.TrimSpace(n); s != "" && s != "0" {
					return s
				}
			case float64:
				if n > 0 {
					return fmt.Sprintf("%.0f", n)
				}
			case int:
				if n > 0 {
					return fmt.Sprintf("%d", n)
				}
			}
		}
	}
	return ""
}

// extractOrderURL 对齐上游 get_order_detail_url。
func extractOrderURL(ret map[string]interface{}) string {
	id := extractOrderID(ret)
	if id == "" {
		return ""
	}
	return fmt.Sprintf("%s/platform/orderDetail.html?order_id=%s", baseURL, id)
}

func extractPayMoney(ret map[string]interface{}) (int, bool) {
	data, ok := ret["data"].(map[string]interface{})
	if !ok {
		return 0, false
	}
	switch v := data["pay_money"].(type) {
	case float64:
		return int(v), true
	case int:
		return v, true
	default:
		return 0, false
	}
}
