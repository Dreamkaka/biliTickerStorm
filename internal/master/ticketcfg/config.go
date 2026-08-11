package ticketcfg

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
)

// 与 biliTickerBuy 导出 JSON 完全一致（字段名、顺序、cookies 仅 name/value）
// 参考用户样例 / tab/settings.on_submit_all + json.dump(indent=4, ensure_ascii=False)

type CookieNV struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type DeliverInfo struct {
	Name   string `json:"name"`
	Tel    string `json:"tel"`
	AddrID int    `json:"addr_id"`
	Addr   string `json:"addr"`
}

// BuyerInfo 字段顺序对齐 Buy / B 站 buyer list 样例
type BuyerInfo struct {
	ID             int    `json:"id"`
	UID            int    `json:"uid"`
	AccountChannel string `json:"account_channel"`
	PersonalID     string `json:"personal_id"`
	Name           string `json:"name"`
	IDCardFront    string `json:"id_card_front"`
	IDCardBack     string `json:"id_card_back"`
	IsDefault      int    `json:"is_default"`
	Tel            string `json:"tel"`
	ErrorCode      string `json:"error_code"`
	IDType         int    `json:"id_type"`
	VerifyStatus   int    `json:"verify_status"`
	AccountID      int    `json:"accountId"`
}

// TicketConfig 字段顺序 = Buy 落盘顺序（勿改 struct 字段顺序）
type TicketConfig struct {
	Username     string       `json:"username"`
	Detail       string       `json:"detail"`
	Count        int          `json:"count"`
	ScreenID     int          `json:"screen_id"`
	ProjectID    int          `json:"project_id"`
	IsHotProject bool         `json:"is_hot_project"`
	SkuID        int          `json:"sku_id"`
	SaleStart    string       `json:"sale_start"`
	OrderType    int          `json:"order_type"`
	PayMoney     int          `json:"pay_money"`
	BuyerInfo    []BuyerInfo  `json:"buyer_info"`
	Buyer        string       `json:"buyer"`
	Tel          string       `json:"tel"`
	DeliverInfo  DeliverInfo  `json:"deliver_info"`
	Cookies      []CookieNV   `json:"cookies"`
	Phone        string       `json:"phone"`
	LinkID       interface{}  `json:"link_id,omitempty"`
}

type Cookie struct {
	Name     string  `json:"name"`
	Value    string  `json:"value"`
	Domain   string  `json:"domain,omitempty"`
	Path     string  `json:"path,omitempty"`
	Expires  float64 `json:"expires,omitempty"`
	HttpOnly bool    `json:"httpOnly,omitempty"`
	Secure   bool    `json:"secure,omitempty"`
	SameSite string  `json:"sameSite,omitempty"`
}

type SelectionInput struct {
	Username     string
	ProjectName  string
	ProjectID    int
	Phone        string
	Ticket       map[string]interface{}
	Buyers       []map[string]interface{}
	Address      map[string]interface{}
	BuyerName    string
	BuyerTel     string
	Cookies      []Cookie
	FileNameHint string
}

// BuildFromSelection 生成与 Buy 样例一致的配置
func BuildFromSelection(in SelectionInput) (*TicketConfig, string, error) {
	if in.Ticket == nil {
		return nil, "", fmt.Errorf("票档不能为空")
	}
	if len(in.Buyers) == 0 {
		return nil, "", fmt.Errorf("请至少选择一位实名购票人")
	}
	for i, b := range in.Buyers {
		if strField(b, "name") == "" || strField(b, "personal_id") == "" {
			return nil, "", fmt.Errorf("buyer_info[%d] 缺少 name 或 personal_id", i)
		}
	}
	if in.Address == nil {
		return nil, "", fmt.Errorf("请选择收货地址")
	}
	if strings.TrimSpace(in.BuyerName) == "" {
		return nil, "", fmt.Errorf("请填写联系人姓名")
	}
	if strings.TrimSpace(in.BuyerTel) == "" {
		return nil, "", fmt.Errorf("请填写联系人电话")
	}
	cookies := CookiesNameValueOnly(in.Cookies)
	if len(cookies) == 0 {
		return nil, "", fmt.Errorf("cookies 不能为空，请先登录")
	}

	count := len(in.Buyers)
	price := intFromAny(in.Ticket["price"])
	screenID := intFromAny(in.Ticket["screen_id"])
	skuID := intFromAny(in.Ticket["id"])
	projectID := intFromAny(in.Ticket["project_id"])
	if projectID == 0 {
		projectID = in.ProjectID
	}
	if screenID == 0 || skuID == 0 || projectID == 0 {
		return nil, "", fmt.Errorf("票档缺少 screen_id / sku_id / project_id")
	}

	username := in.Username
	if username == "" {
		username = "unknown-user"
	}
	projectName := in.ProjectName
	if projectName == "" {
		projectName = "unknown-project"
	}
	// Buy detail: username-projectName-ticketLabel-buyerNames
	// ticketLabel 与 settings 里 ticket_str_list 一致（含场次/票种/价格/状态/起售）
	ticketLabel := strField(in.Ticket, "display")
	if ticketLabel == "" {
		ticketLabel = strField(in.Ticket, "desc")
	}
	if ticketLabel == "" {
		ticketLabel = fmt.Sprintf("sku-%d", skuID)
	}

	detail := username + "-" + projectName + "-" + ticketLabel
	for _, b := range in.Buyers {
		if n := strField(b, "name"); n != "" {
			detail += "-" + n
		}
	}
	detail = strings.Trim(detail, "-")

	addr := in.Address
	deliver := DeliverInfo{
		Name:   strField(addr, "name"),
		Tel:    firstNonEmpty(strField(addr, "phone"), strField(addr, "tel")),
		AddrID: intFromAny(firstAny(addr["id"], addr["addr_id"])),
		Addr: strings.Join([]string{
			strField(addr, "prov"),
			strField(addr, "city"),
			strField(addr, "area"),
			firstNonEmpty(strField(addr, "addr"), strField(addr, "address")),
		}, ""),
	}
	if deliver.Name == "" || deliver.Tel == "" || deliver.Addr == "" {
		return nil, "", fmt.Errorf("deliver_info 不完整（name/tel/addr）")
	}

	saleStart := formatSaleStart(firstAny(in.Ticket["sale_start"], in.Ticket["saleStart"]))

	isHot := false
	if v, ok := in.Ticket["is_hot_project"].(bool); ok {
		isHot = v
	}

	buyers := make([]BuyerInfo, 0, len(in.Buyers))
	for _, b := range in.Buyers {
		buyers = append(buyers, buyerFromMap(b))
	}

	cfg := &TicketConfig{
		Username:     username,
		Detail:       detail,
		Count:        count,
		ScreenID:     screenID,
		ProjectID:    projectID,
		IsHotProject: isHot,
		SkuID:        skuID,
		SaleStart:    saleStart,
		OrderType:    1,
		PayMoney:     price * count,
		BuyerInfo:    buyers,
		Buyer:        strings.TrimSpace(in.BuyerName),
		Tel:          strings.TrimSpace(in.BuyerTel),
		DeliverInfo:  deliver,
		Cookies:      cookies,
		Phone:        in.Phone, // 始终输出，可为空串
	}
	if lid := firstAny(in.Ticket["link_id"], nil); lid != nil && lid != "" {
		cfg.LinkID = lid
	}

	if err := ValidateConfig(cfg); err != nil {
		return nil, "", err
	}

	fileBase := in.FileNameHint
	if fileBase == "" {
		fileBase = detail
	}
	fileBase = FilenameFilter(fileBase)
	if fileBase == "" {
		fileBase = fmt.Sprintf("project-%d-sku-%d", projectID, skuID)
	}
	return cfg, fileBase, nil
}

func ValidateConfig(cfg *TicketConfig) error {
	if cfg == nil {
		return fmt.Errorf("config is nil")
	}
	if cfg.Detail == "" || cfg.Count <= 0 || cfg.ScreenID == 0 || cfg.ProjectID == 0 || cfg.SkuID == 0 {
		return fmt.Errorf("missing required core fields")
	}
	if len(cfg.BuyerInfo) == 0 {
		return fmt.Errorf("buyer_info must be a non-empty list")
	}
	for i, b := range cfg.BuyerInfo {
		if b.Name == "" || b.PersonalID == "" {
			return fmt.Errorf("buyer_info[%d] missing name or personal_id", i)
		}
	}
	if cfg.Buyer == "" || cfg.Tel == "" {
		return fmt.Errorf("buyer/tel required")
	}
	if cfg.DeliverInfo.Name == "" || cfg.DeliverInfo.Tel == "" || cfg.DeliverInfo.Addr == "" {
		return fmt.Errorf("deliver_info incomplete")
	}
	if len(cfg.Cookies) == 0 {
		return fmt.Errorf("cookies must be a non-empty list")
	}
	for i, c := range cfg.Cookies {
		if c.Name == "" || c.Value == "" {
			return fmt.Errorf("cookies[%d] missing name or value", i)
		}
	}
	return nil
}

// Validate 兼容 map 校验（上传路径）
func Validate(cfg map[string]interface{}) error {
	required := []string{
		"detail", "count", "screen_id", "project_id", "sku_id", "pay_money",
		"buyer_info", "buyer", "tel", "deliver_info", "cookies",
	}
	for _, k := range required {
		v, ok := cfg[k]
		if !ok || v == nil || v == "" {
			return fmt.Errorf("missing required field: %s", k)
		}
	}
	return nil
}

// FilenameFilter 对齐 Buy：去掉 \ / : * ? " < > |
func FilenameFilter(name string) string {
	name = strings.TrimSpace(name)
	name = strings.TrimSuffix(name, ".json")
	var b strings.Builder
	for _, r := range name {
		switch r {
		case '\\', '/', ':', '*', '?', '"', '<', '>', '|':
		default:
			if r == 0 || !unicode.IsPrint(r) {
				continue
			}
			b.WriteRune(r)
		}
	}
	out := strings.TrimSpace(b.String())
	if out == "." || out == ".." {
		return ""
	}
	return out
}

// CookiesNameValueOnly 仅 name+value（与 Buy CookieManager / 样例完全一致）
func CookiesNameValueOnly(in []Cookie) []CookieNV {
	seen := make(map[string]string)
	order := make([]string, 0)
	for _, c := range in {
		name := strings.TrimSpace(c.Name)
		if name == "" || c.Value == "" {
			continue
		}
		if _, ok := seen[name]; !ok {
			order = append(order, name)
		}
		seen[name] = c.Value
	}
	out := make([]CookieNV, 0, len(order))
	for _, n := range order {
		out = append(out, CookieNV{Name: n, Value: seen[n]})
	}
	return out
}

// NormalizeCookies 账号存储仍可保留扩展字段；导出配置时用 CookiesNameValueOnly
func NormalizeCookies(in []Cookie) []Cookie {
	seen := make(map[string]Cookie)
	order := make([]string, 0)
	for _, c := range in {
		c.Name = strings.TrimSpace(c.Name)
		if c.Name == "" || c.Value == "" {
			continue
		}
		if _, ok := seen[c.Name]; !ok {
			order = append(order, c.Name)
		}
		seen[c.Name] = c
	}
	out := make([]Cookie, 0, len(order))
	for _, n := range order {
		out = append(out, seen[n])
	}
	return out
}

// MarshalBuyJSON 对 TicketConfig：indent=4，中文不转义，字段顺序固定
func MarshalBuyJSON(v interface{}) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "    ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	b := buf.Bytes()
	if len(b) > 0 && b[len(b)-1] == '\n' {
		b = b[:len(b)-1]
	}
	return b, nil
}

// MarshalConfigFile 专用：保证输出与 Buy 样例一致
func MarshalConfigFile(cfg *TicketConfig) ([]byte, error) {
	return MarshalBuyJSON(cfg)
}

func CookiesFromBiliAPI(raw interface{}) []Cookie {
	switch t := raw.(type) {
	case []Cookie:
		return NormalizeCookies(t)
	case []CookieNV:
		out := make([]Cookie, 0, len(t))
		for _, c := range t {
			out = append(out, Cookie{Name: c.Name, Value: c.Value})
		}
		return out
	case []map[string]interface{}:
		out := make([]Cookie, 0, len(t))
		for _, m := range t {
			out = append(out, Cookie{Name: strField(m, "name"), Value: strField(m, "value")})
		}
		return NormalizeCookies(out)
	case []interface{}:
		out := make([]Cookie, 0, len(t))
		for _, item := range t {
			if m, ok := item.(map[string]interface{}); ok {
				out = append(out, Cookie{Name: strField(m, "name"), Value: strField(m, "value")})
			}
		}
		return NormalizeCookies(out)
	default:
		return nil
	}
}

func buyerFromMap(m map[string]interface{}) BuyerInfo {
	return BuyerInfo{
		ID:             intFromAny(m["id"]),
		UID:            intFromAny(m["uid"]),
		AccountChannel: strField(m, "account_channel"),
		PersonalID:     strField(m, "personal_id"),
		Name:           strField(m, "name"),
		IDCardFront:    strField(m, "id_card_front"),
		IDCardBack:     strField(m, "id_card_back"),
		IsDefault:      intFromAny(m["is_default"]),
		Tel:            strField(m, "tel"),
		ErrorCode:      strField(m, "error_code"),
		IDType:         intFromAny(m["id_type"]),
		VerifyStatus:   intFromAny(m["verify_status"]),
		AccountID:      intFromAny(firstAny(m["accountId"], m["account_id"])),
	}
}

func strField(m map[string]interface{}, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	default:
		return strings.TrimSpace(fmt.Sprint(t))
	}
}

func intFromAny(v interface{}) int {
	switch t := v.(type) {
	case int:
		return t
	case int64:
		return int(t)
	case float64:
		return int(t)
	case json.Number:
		i, _ := t.Int64()
		return int(i)
	case string:
		var n int
		_, _ = fmt.Sscanf(t, "%d", &n)
		return n
	default:
		return 0
	}
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

func firstAny(vals ...interface{}) interface{} {
	for _, v := range vals {
		if v == nil {
			continue
		}
		if s, ok := v.(string); ok && s == "" {
			continue
		}
		return v
	}
	return nil
}

func formatSaleStart(v interface{}) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case float64:
		if t == float64(int64(t)) {
			return fmt.Sprintf("%d", int64(t))
		}
		return fmt.Sprintf("%v", t)
	case int:
		return fmt.Sprintf("%d", t)
	case int64:
		return fmt.Sprintf("%d", t)
	case json.Number:
		return t.String()
	default:
		s := strings.TrimSpace(fmt.Sprint(t))
		if s == "<nil>" {
			return ""
		}
		return s
	}
}
