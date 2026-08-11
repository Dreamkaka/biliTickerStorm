package biliapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	passportBase = "https://passport.bilibili.com"
	showBase     = "https://show.bilibili.com"
	userAgent    = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
)

type Client struct {
	HTTP *http.Client
}

func NewClient() *Client {
	return &Client{
		HTTP: &http.Client{Timeout: 20 * time.Second},
	}
}

func (c *Client) do(method, rawURL string, cookies string, body io.Reader, contentType string) ([]byte, []*http.Cookie, error) {
	req, err := http.NewRequest(method, rawURL, body)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Referer", "https://show.bilibili.com/")
	if cookies != "" {
		req.Header.Set("Cookie", cookies)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, err
	}
	return b, resp.Cookies(), nil
}

type QRStartResult struct {
	URL       string `json:"url"`
	QRCodeKey string `json:"qrcode_key"`
}

func (c *Client) StartQRLogin() (*QRStartResult, error) {
	b, _, err := c.do(http.MethodGet, passportBase+"/x/passport-login/web/qrcode/generate", "", nil, "")
	if err != nil {
		return nil, err
	}
	var resp struct {
		Code int `json:"code"`
		Data struct {
			URL       string `json:"url"`
			QRCodeKey string `json:"qrcode_key"`
		} `json:"data"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(b, &resp); err != nil {
		return nil, err
	}
	if resp.Code != 0 {
		return nil, fmt.Errorf("qr generate failed: %s", resp.Message)
	}
	return &QRStartResult{URL: resp.Data.URL, QRCodeKey: resp.Data.QRCodeKey}, nil
}

type QRPollResult struct {
	Code    int               `json:"code"` // 0 成功, 86038 失效, 86090 已扫, 86101 未扫
	Message string            `json:"message"`
	Cookies []Cookie          `json:"cookies,omitempty"`
	CookieHeader string       `json:"cookie_header,omitempty"`
}

type Cookie struct {
	Name     string  `json:"name"`
	Value    string  `json:"value"`
	Domain   string  `json:"domain"`
	Path     string  `json:"path"`
	Expires  float64 `json:"expires"`
	HttpOnly bool    `json:"httpOnly"`
	Secure   bool    `json:"secure"`
	SameSite string  `json:"sameSite"`
}

func (c *Client) PollQRLogin(qrcodeKey string) (*QRPollResult, error) {
	u := passportBase + "/x/passport-login/web/qrcode/poll?qrcode_key=" + url.QueryEscape(qrcodeKey)
	b, setCookies, err := c.do(http.MethodGet, u, "", nil, "")
	if err != nil {
		return nil, err
	}
	var resp struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
			URL     string `json:"url"`
		} `json:"data"`
	}
	if err := json.Unmarshal(b, &resp); err != nil {
		return nil, err
	}
	// 外层 code 0，业务在 data.code
	dataCode := resp.Data.Code
	if resp.Code != 0 && dataCode == 0 {
		dataCode = resp.Code
	}
	out := &QRPollResult{Code: dataCode, Message: resp.Data.Message}
	if out.Message == "" {
		out.Message = resp.Message
	}
	if dataCode != 0 {
		return out, nil
	}
	// 成功：从 Set-Cookie 与跳转 URL 提取
	cookies := make([]Cookie, 0)
	for _, sc := range setCookies {
		cookies = append(cookies, Cookie{
			Name:     sc.Name,
			Value:    sc.Value,
			Domain:   sc.Domain,
			Path:     sc.Path,
			Expires:  float64(sc.Expires.Unix()),
			HttpOnly: sc.HttpOnly,
			Secure:   sc.Secure,
		})
	}
	// poll 成功时 cookie 常在响应 data 外的 set-cookie；部分环境 cookie 在 url 参数
	if resp.Data.URL != "" {
		if u, err := url.Parse(resp.Data.URL); err == nil {
			for k, vs := range u.Query() {
				if k == "gourl" {
					continue
				}
				for _, v := range vs {
					cookies = append(cookies, Cookie{
						Name: k, Value: v, Domain: ".bilibili.com", Path: "/",
					})
				}
			}
		}
	}
	out.Cookies = cookies
	out.CookieHeader = cookiesToHeader(cookies)
	return out, nil
}

func cookiesToHeader(cookies []Cookie) string {
	parts := make([]string, 0, len(cookies))
	for _, c := range cookies {
		if c.Name == "" {
			continue
		}
		parts = append(parts, c.Name+"="+c.Value)
	}
	return strings.Join(parts, "; ")
}

func CookiesToHeader(cookies []Cookie) string {
	return cookiesToHeader(cookies)
}

func (c *Client) FetchUsername(cookieHeader string) (string, error) {
	b, _, err := c.do(http.MethodGet, "https://api.bilibili.com/x/web-interface/nav", cookieHeader, nil, "")
	if err != nil {
		return "", err
	}
	var resp struct {
		Code int `json:"code"`
		Data struct {
			Uname string `json:"uname"`
			Mid   int64  `json:"mid"`
		} `json:"data"`
	}
	if err := json.Unmarshal(b, &resp); err != nil {
		return "", err
	}
	if resp.Code != 0 {
		return "", fmt.Errorf("nav code=%d", resp.Code)
	}
	if resp.Data.Uname != "" {
		return resp.Data.Uname, nil
	}
	return fmt.Sprintf("uid-%d", resp.Data.Mid), nil
}

func (c *Client) GetProject(projectID int, cookieHeader string) (map[string]interface{}, error) {
	u := fmt.Sprintf("%s/api/ticket/project/getV2?version=134&id=%d&project_id=%d&requestSource=neul-next", showBase, projectID, projectID)
	b, _, err := c.do(http.MethodGet, u, cookieHeader, nil, "")
	if err != nil {
		return nil, err
	}
	var resp struct {
		Errno   int                    `json:"errno"`
		Code    int                    `json:"code"`
		Msg     string                 `json:"msg"`
		Message string                 `json:"message"`
		Data    map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(b, &resp); err != nil {
		return nil, err
	}
	code := resp.Errno
	if code == 0 && resp.Code != 0 {
		code = resp.Code
	}
	if code != 0 {
		msg := resp.Msg
		if msg == "" {
			msg = resp.Message
		}
		return nil, fmt.Errorf("project api: %s", msg)
	}
	return resp.Data, nil
}

func (c *Client) GetBuyers(cookieHeader string) ([]map[string]interface{}, error) {
	u := showBase + "/api/ticket/buyer/list"
	b, _, err := c.do(http.MethodGet, u, cookieHeader, nil, "")
	if err != nil {
		return nil, err
	}
	var resp struct {
		Errno int `json:"errno"`
		Code  int `json:"code"`
		Data  struct {
			List []map[string]interface{} `json:"list"`
		} `json:"data"`
		Msg     string `json:"msg"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(b, &resp); err != nil {
		return nil, err
	}
	code := resp.Errno
	if code == 0 && resp.Code != 0 {
		code = resp.Code
	}
	if code != 0 {
		msg := resp.Msg
		if msg == "" {
			msg = resp.Message
		}
		return nil, fmt.Errorf("buyer api: %s", msg)
	}
	return resp.Data.List, nil
}

func (c *Client) GetAddresses(cookieHeader string) ([]map[string]interface{}, error) {
	u := showBase + "/api/ticket/addr/list"
	b, _, err := c.do(http.MethodGet, u, cookieHeader, nil, "")
	if err != nil {
		return nil, err
	}
	var resp struct {
		Errno int `json:"errno"`
		Code  int `json:"code"`
		Data  struct {
			AddrList []map[string]interface{} `json:"addr_list"`
			List     []map[string]interface{} `json:"list"`
		} `json:"data"`
		Msg     string `json:"msg"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(b, &resp); err != nil {
		return nil, err
	}
	code := resp.Errno
	if code == 0 && resp.Code != 0 {
		code = resp.Code
	}
	if code != 0 {
		msg := resp.Msg
		if msg == "" {
			msg = resp.Message
		}
		return nil, fmt.Errorf("addr api: %s", msg)
	}
	if len(resp.Data.AddrList) > 0 {
		return resp.Data.AddrList, nil
	}
	return resp.Data.List, nil
}

// BuildTicketOptions 从 project data 提取票档选项
func BuildTicketOptions(project map[string]interface{}) []map[string]interface{} {
	options := make([]map[string]interface{}, 0)
	screens, _ := project["screen_list"].([]interface{})
	hot := false
	if v, ok := project["hotProject"].(bool); ok {
		hot = v
	}
	if v, ok := project["hot_project"].(bool); ok {
		hot = hot || v
	}
	hasEticket := false
	if v, ok := project["has_eticket"].(float64); ok {
		hasEticket = v > 0
	}
	projectID := project["id"]

	for _, s := range screens {
		screen, ok := s.(map[string]interface{})
		if !ok {
			continue
		}
		screenName, _ := screen["name"].(string)
		screenID := screen["id"]
		expressFee := 0.0
		if !hasEticket {
			if ef, ok := screen["express_fee"].(float64); ok && ef > 0 {
				expressFee = ef
			}
		}
		tickets, _ := screen["ticket_list"].([]interface{})
		for _, t := range tickets {
			ticket, ok := t.(map[string]interface{})
			if !ok {
				continue
			}
			price, _ := ticket["price"].(float64)
			price += expressFee
			desc, _ := ticket["desc"].(string)
			saleStart := ticket["sale_start"]
			if saleStart == nil || saleStart == "" {
				saleStart = ticket["saleStart"]
			}
			// 展示用字符串；落盘仍保留原始 sale_start（BuildFromSelection 读取）
			saleStartDisp := ""
			if saleStart != nil {
				saleStartDisp = fmt.Sprint(saleStart)
			}
			display := fmt.Sprintf("%s - %s - ￥%.2f - 【起售时间：%s】", screenName, desc, price/100, saleStartDisp)
			opt := map[string]interface{}{
				"id":             ticket["id"],
				"desc":           desc,
				"price":          price,
				"screen":         screenName,
				"screen_id":      screenID,
				"project_id":     projectID,
				"is_hot_project": hot,
				"sale_start":     saleStart,
				"display":        display,
			}
			if lid := screen["link_id"]; lid != nil && lid != "" {
				opt["link_id"] = lid
			}
			options = append(options, opt)
		}
	}
	return options
}
