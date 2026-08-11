package worker

import "testing"

func TestSimCTokenStateAdvancesTimer(t *testing.T) {
	win := BrowserWindowState{
		InnerWidth: 1200, InnerHeight: 800,
		OuterWidth: 1280, OuterHeight: 900,
		ScreenWidth: 1920, ScreenHeight: 1080,
		ScreenAvailWidth: 1920, ScreenAvailHeight: 1040,
	}
	const t0 int64 = 1_700_000_000_000
	st := InitCTokenState(win, 0, len(defaultUserAgent), len(mobileDetailHref(1)), t0)
	snap0 := st.Snapshot(t0)
	snap1 := SimCTokenState(st, t0+5000)
	if snap1.Timer <= snap0.Timer {
		t.Fatalf("timer should advance: %d -> %d", snap0.Timer, snap1.Timer)
	}
	if snap1.Timediff <= snap0.Timediff {
		t.Fatalf("timediff should advance: %v -> %v", snap0.Timediff, snap1.Timediff)
	}
	// ticket_collection_t 固定
	if snap0.TicketCollectionT != t0 || snap1.TicketCollectionT != t0 {
		t.Fatalf("ticket_collection_t: %d %d", snap0.TicketCollectionT, snap1.TicketCollectionT)
	}
}

func TestPrepareAndCreateCTokenDecodable(t *testing.T) {
	win := BrowserWindowState{
		InnerWidth: 1000, InnerHeight: 700,
		OuterWidth: 1100, OuterHeight: 800,
		ScreenWidth: 1920, ScreenHeight: 1080,
		ScreenAvailWidth: 1920, ScreenAvailHeight: 1040,
	}
	href := mobileDetailHref(42)
	st := InitCTokenState(win, 0, len(defaultUserAgent), len(href), 1_700_000_000_000)
	prep := st.Snapshot(1_700_000_000_000).GeneratePrepareCToken()
	create := SimCTokenState(st, 1_700_000_001_000).GenerateCreateCToken()
	if prep == "" || create == "" {
		t.Fatal("empty ctoken")
	}
	if prep == create {
		// 通常不同（openWindow/beforeunload 与 timer），但不强制；至少均可生成
		t.Log("prepare/create ctoken equal (possible with unlucky rand)")
	}
}
