package worker

import (
	"time"
)

// riskCooldown 计算本地 412 冷却时长（递增，上限 maxSeconds）。
func riskCooldown(attempt, baseMs, maxSeconds int) time.Duration {
	if baseMs <= 0 {
		baseMs = 1000
	}
	if maxSeconds <= 0 {
		maxSeconds = 30
	}
	ms := baseMs * attempt
	maxMs := maxSeconds * 1000
	if ms > maxMs {
		ms = maxMs
	}
	if ms < baseMs {
		ms = baseMs
	}
	return time.Duration(ms) * time.Millisecond
}
