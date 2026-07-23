package worker

import (
	"encoding/base64"
	"math/rand"
	"time"
)

// 对齐 biliTickerBuy/cptoken：prepare/create 所需 ctoken 生成。

type BrowserWindowState struct {
	ScrollX           int
	ScrollY           int
	InnerWidth        int
	InnerHeight       int
	OuterWidth        int
	OuterHeight       int
	ScreenX           int
	ScreenY           int
	ScreenWidth       int
	ScreenHeight      int
	ScreenAvailWidth  int
	ScreenAvailHeight int
}

type CTokenSnapshot struct {
	M1                int
	Touchend          int
	M2                int
	Visibilitychange  int
	M3                int
	M4                int
	OpenWindow        int
	M5                int
	Timer             int
	Timediff          float64
	M6                int
	M7                int
	M8                int
	M9                int
	Beforeunload      int
	TicketCollectionT int64
	BaseTimer         int
}

type CTokenRuntimeState struct {
	M1                int
	Touchend          int
	M2                int
	Visibilitychange  int
	M3                int
	M4                int
	OpenWindow        int
	M5                int
	M6                int
	M7                int
	M8                int
	M9                int
	Beforeunload      int
	TicketCollectionT int64
	BaseTimer         int
	BaseTimediff      float64
	CreatedAtMs       int64
}

func b1(x int) byte {
	if x < 0 || x > 255 {
		return 0xff
	}
	return byte(x)
}

func generateCToken(
	m1, touchend, m2, visibilitychange, m3, m4, openWindow, m5, timer int,
	timediff float64,
	m6, m7, m8, m9, beforeunload int,
) string {
	if touchend == -1 {
		touchend = 30 + rand.Intn(21)
	}
	if visibilitychange == -1 {
		visibilitychange = 10 + rand.Intn(41)
	}
	if beforeunload == -1 {
		if openWindow != -1 {
			beforeunload = openWindow
		} else {
			beforeunload = 10 + rand.Intn(41)
		}
	}
	if timer == -1 {
		timer = 1 + rand.Intn(10)
	}

	tb := []byte{
		b1(m1), 0x00,
		b1(touchend), 0x00,
		b1(m2), 0x00,
		b1(visibilitychange), 0x00,
		b1(m3), 0x00,
		b1(m4), 0x00,
		b1(beforeunload), 0x00,
		b1(m5), 0x00,
	}
	if timer < 0 || timer > 0xffff {
		tb = append(tb, 0xff, 0x00, 0xff, 0x00)
	} else {
		tt := uint16(timer)
		tb = append(tb, b1(int(tt>>8)), 0x00, b1(int(tt&0xff)), 0x00)
	}
	tcVal := int(timediff)
	if tcVal < 0 || tcVal > 0xffff {
		tb = append(tb, 0xff, 0x00, 0xff, 0x00)
	} else {
		tc := uint16(tcVal)
		tb = append(tb, b1(int(tc>>8)), 0x00, b1(int(tc&0xff)), 0x00)
	}
	tb = append(tb,
		b1(m6), 0x00,
		b1(m7), 0x00,
		b1(m8), 0x00,
		b1(m9), 0x00,
	)
	return base64.StdEncoding.EncodeToString(tb)
}

func (s CTokenSnapshot) GeneratePrepareCToken() string {
	return generateCToken(
		s.M1, s.Touchend, s.M2, s.Visibilitychange, s.M3, s.M4,
		s.OpenWindow, s.M5, s.Timer, s.Timediff,
		s.M6, s.M7, s.M8, s.M9, s.Beforeunload,
	)
}

func (s CTokenSnapshot) GenerateCreateCToken() string {
	// create 不传 openWindow/beforeunload，走默认随机（与主项目一致）
	return generateCToken(
		s.M1, s.Touchend, s.M2, s.Visibilitychange, s.M3, s.M4,
		-1, s.M5, s.Timer, s.Timediff,
		s.M6, s.M7, s.M8, s.M9, -1,
	)
}

func GenerateBrowserWindowState() BrowserWindowState {
	screens := [][2]int{
		{1920, 1080}, {2560, 1440}, {1366, 768}, {1440, 900},
		{1536, 864}, {1600, 900}, {1280, 720},
	}
	sc := screens[rand.Intn(len(screens))]
	screenW, screenH := sc[0], sc[1]
	taskbar := []int{40, 48, 56, 64}[rand.Intn(4)]
	availW, availH := screenW, screenH-taskbar
	maximized := rand.Float64() < 0.65
	chromeW := []int{0, 8, 12, 16}[rand.Intn(4)]
	chromeH := []int{80, 88, 96, 104, 112, 120}[rand.Intn(6)]

	var outerW, outerH, screenX, screenY, innerW, innerH int
	if maximized {
		outerW, outerH = availW, availH
		screenX, screenY = 0, 0
		innerW = outerW - chromeW
		innerH = outerH - chromeH
	} else {
		outerW = int(float64(availW)*0.60) + rand.Intn(max(1, int(float64(availW)*0.30)))
		outerH = int(float64(availH)*0.60) + rand.Intn(max(1, int(float64(availH)*0.30)))
		maxX := max(0, availW-outerW)
		maxY := max(0, availH-outerH)
		if maxX > 0 {
			screenX = rand.Intn(maxX + 1)
		}
		if maxY > 0 {
			screenY = rand.Intn(maxY + 1)
		}
		innerW = outerW - chromeW
		innerH = outerH - chromeH
	}
	if innerW < 320 {
		innerW = 320
	}
	if innerH < 240 {
		innerH = 240
	}
	return BrowserWindowState{
		ScrollX:           0,
		ScrollY:           0,
		InnerWidth:        innerW,
		InnerHeight:       innerH,
		OuterWidth:        outerW,
		OuterHeight:       outerH,
		ScreenX:           screenX,
		ScreenY:           screenY,
		ScreenWidth:       screenW,
		ScreenHeight:      screenH,
		ScreenAvailWidth:  availW,
		ScreenAvailHeight: availH,
	}
}

func (st *CTokenRuntimeState) Snapshot(nowMs int64) CTokenSnapshot {
	if nowMs == 0 {
		nowMs = time.Now().UnixMilli()
	}
	elapsed := float64(nowMs-st.CreatedAtMs) / 1000
	if elapsed < 0 {
		elapsed = 0
	}
	timediff := st.BaseTimediff
	if st.TicketCollectionT > 0 {
		d := float64(nowMs-st.TicketCollectionT) / 1000
		if d > 0 {
			timediff += d
		}
	}
	return CTokenSnapshot{
		M1: st.M1, Touchend: st.Touchend, M2: st.M2,
		Visibilitychange: st.Visibilitychange, M3: st.M3, M4: st.M4,
		OpenWindow: st.OpenWindow, M5: st.M5,
		Timer: st.BaseTimer + int(elapsed), Timediff: timediff,
		M6: st.M6, M7: st.M7, M8: st.M8, M9: st.M9,
		Beforeunload: st.Beforeunload, TicketCollectionT: st.TicketCollectionT,
		BaseTimer: st.BaseTimer,
	}
}

func InitCTokenState(browser BrowserWindowState, historyLength, userAgentLength, hrefLength int, ticketCollectionT int64) *CTokenRuntimeState {
	if historyLength <= 0 {
		historyLength = 2 + rand.Intn(9)
	}
	if userAgentLength <= 0 {
		userAgentLength = 140
	}
	if hrefLength <= 0 {
		hrefLength = 76
	}
	devicePixelRatio := 4.0
	nowMod := int(time.Now().UnixMilli() % 256)
	values := []int{
		browser.ScrollX, browser.ScrollY,
		browser.InnerWidth, browser.InnerHeight,
		browser.OuterWidth, browser.OuterHeight,
		browser.ScreenX, browser.ScreenY,
		browser.ScreenWidth, browser.ScreenHeight,
		browser.ScreenAvailWidth, historyLength,
		userAgentLength, hrefLength,
		int(10 * devicePixelRatio), nowMod,
	}
	deriveD := func(index int) int {
		return (values[index%16] + values[(3*index)%16] + 17*index) & 255
	}
	created := ticketCollectionT
	if created == 0 {
		created = time.Now().UnixMilli()
	}
	return &CTokenRuntimeState{
		M1: deriveD(1), Touchend: 0, M2: deriveD(2), Visibilitychange: 0,
		M3: deriveD(3), M4: deriveD(4), OpenWindow: 1 + rand.Intn(3), M5: deriveD(5),
		M6: deriveD(6), M7: deriveD(7), M8: deriveD(8), M9: deriveD(9),
		Beforeunload: 1 + rand.Intn(3), TicketCollectionT: ticketCollectionT,
		BaseTimer: 10 + rand.Intn(91), CreatedAtMs: created,
	}
}

func SimCTokenState(before *CTokenRuntimeState, nowMs int64) CTokenSnapshot {
	if nowMs == 0 {
		nowMs = time.Now().UnixMilli()
	}
	source := before.Snapshot(before.CreatedAtMs)
	ticketT := source.TicketCollectionT
	baseTimer := source.BaseTimer
	if baseTimer == 0 {
		baseTimer = source.Timer
	}
	touchAdd := []int{0, 0, 1, 2}[rand.Intn(4)]
	openAdd := 0
	if r := rand.Intn(100); r >= 80 {
		openAdd = 1
	}
	visAdd := 0
	if r := rand.Intn(100); r >= 80 {
		visAdd = 1
	}
	timediff := float64(0)
	if ticketT > 0 {
		timediff = float64(nowMs-ticketT) / 1000
		if timediff < 0 {
			timediff = 0
		}
	}
	timer := baseTimer
	if ticketT > 0 {
		timer = baseTimer + int((nowMs-ticketT)/1000)
	}
	return CTokenSnapshot{
		M1: source.M1, Touchend: source.Touchend + touchAdd, M2: source.M2,
		Visibilitychange: source.Visibilitychange + visAdd, M3: source.M3, M4: source.M4,
		OpenWindow: source.OpenWindow + openAdd, M5: source.M5,
		Timer: timer, Timediff: timediff,
		M6: source.M6, M7: source.M7, M8: source.M8, M9: source.M9,
		TicketCollectionT: ticketT, BaseTimer: baseTimer,
	}
}

func normalizePreparePToken(v interface{}) string {
	if v == nil {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] != '=' {
			out = append(out, s[i])
		}
	}
	return string(out)
}
