package worker

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// FlexibleTimeString 接受 JSON 字符串或数字（unix 秒/毫秒）
type FlexibleTimeString string

func (f *FlexibleTimeString) UnmarshalJSON(b []byte) error {
	b = bytesTrimSpace(b)
	if len(b) == 0 || string(b) == "null" {
		*f = ""
		return nil
	}
	if b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*f = FlexibleTimeString(s)
		return nil
	}
	// number
	var n json.Number
	if err := json.Unmarshal(b, &n); err != nil {
		return err
	}
	*f = FlexibleTimeString(n.String())
	return nil
}

func bytesTrimSpace(b []byte) []byte {
	return []byte(strings.TrimSpace(string(b)))
}

// ParseSaleStart 对齐 biliTickerBuy tab/go.py _parse_sale_start
// 支持 unix 秒/毫秒数字字符串，以及 "2006-01-02 15:04:05" / "2006-01-02T15:04:05" / "2006-01-02T15:04"
func ParseSaleStart(value interface{}, loc *time.Location) (*time.Time, error) {
	if loc == nil {
		loc, _ = time.LoadLocation("Asia/Shanghai")
	}
	if value == nil {
		return nil, nil
	}
	switch v := value.(type) {
	case time.Time:
		t := v.In(loc)
		return &t, nil
	case *time.Time:
		if v == nil {
			return nil, nil
		}
		t := v.In(loc)
		return &t, nil
	case int:
		return unixToTime(int64(v), loc)
	case int64:
		return unixToTime(v, loc)
	case float64:
		return unixToTime(int64(v), loc)
	case string:
		s := strings.TrimSpace(v)
		if s == "" {
			return nil, nil
		}
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			return unixToTime(n, loc)
		}
		if n, err := strconv.ParseFloat(s, 64); err == nil {
			return unixToTime(int64(n), loc)
		}
		for _, layout := range []string{
			"2006-01-02 15:04:05",
			"2006-01-02T15:04:05",
			"2006-01-02T15:04",
			"2006-01-02 15:04",
		} {
			if t, err := time.ParseInLocation(layout, s, loc); err == nil {
				return &t, nil
			}
		}
		return nil, fmt.Errorf("无法解析 sale_start: %q", s)
	default:
		return nil, fmt.Errorf("不支持的 sale_start 类型: %T", value)
	}
}

func unixToTime(n int64, loc *time.Location) (*time.Time, error) {
	// 毫秒
	if n > 1_000_000_000_000 {
		t := time.UnixMilli(n).In(loc)
		return &t, nil
	}
	t := time.Unix(n, 0).In(loc)
	return &t, nil
}

// ResolveTaskStartTime 优先级：env TICKET_TIME_START > 配置 sale_start > nil(立即)
func ResolveTaskStartTime(envStart *time.Time, saleStartRaw string) *time.Time {
	if envStart != nil {
		return envStart
	}
	loc, _ := time.LoadLocation("Asia/Shanghai")
	t, err := ParseSaleStart(saleStartRaw, loc)
	if err != nil || t == nil {
		return nil
	}
	// 已过起售时间则立即开抢（对齐 Buy auto_fill 的「已过则不必填」）
	if !t.After(time.Now().In(loc)) {
		return nil
	}
	return t
}
