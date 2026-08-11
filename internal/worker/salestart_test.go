package worker

import (
	"testing"
	"time"
)

func TestParseSaleStart(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Shanghai")
	cases := []struct {
		in   interface{}
		want string
	}{
		{"2026-08-01 12:00:00", "2026-08-01T12:00:00+08:00"},
		{"2026-08-01T12:00:00", "2026-08-01T12:00:00+08:00"},
		{"2026-08-01T12:00", "2026-08-01T12:00:00+08:00"},
	}
	for _, c := range cases {
		got, err := ParseSaleStart(c.in, loc)
		if err != nil || got == nil {
			t.Fatalf("%v: %v %v", c.in, got, err)
		}
		if got.Format(time.RFC3339) != c.want {
			t.Fatalf("%v => %s want %s", c.in, got.Format(time.RFC3339), c.want)
		}
	}
	// empty
	got, err := ParseSaleStart("", loc)
	if err != nil || got != nil {
		t.Fatalf("empty: %v %v", got, err)
	}
}

func TestResolveTaskStartTime_EnvPriority(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Shanghai")
	env := time.Date(2030, 1, 1, 10, 0, 0, 0, loc)
	got := ResolveTaskStartTime(&env, "2026-08-01 12:00:00")
	if got == nil || !got.Equal(env) {
		t.Fatalf("env should win: %v", got)
	}
	// past sale_start => nil
	past := ResolveTaskStartTime(nil, "2020-01-01 12:00:00")
	if past != nil {
		t.Fatalf("past should be nil: %v", past)
	}
}
