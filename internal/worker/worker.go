package worker

import (
	. "biliTickerStorm/internal/common"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/sirupsen/logrus"
)

type Worker struct {
	m      *Register
	cancel context.CancelFunc
	mu     sync.Mutex // 保证并发安全地访问 cancel
}

func NewWorker(m *Register) *Worker {
	return &Worker{
		m: m,
	}
}

func (w *Worker) RunTask(ctx context.Context, info, taskId string) error {
	w.mu.Lock()
	if w.cancel != nil {
		w.mu.Unlock()
		return fmt.Errorf("已有任务正在执行")
	}
	cancelCtx, cancel := context.WithCancel(context.Background())
	w.cancel = cancel
	w.mu.Unlock()

	var config BiliTickerBuyConfig
	if err := json.Unmarshal([]byte(info), &config); err != nil {
		log.Printf("[ConfigError] BiliTickerBuy: %v", err)
		return fmt.Errorf("解析配置失败: %w", err)
	}
	go func() {
		err := w.m.UpdateWorkerStatusAndTaskStatus(Working, TaskStatusDoing, taskId)
		if err != nil {
			log.WithFields(logrus.Fields{"username": config.Username, "detail": config.Detail}).Warningf("设置状态 Working,TaskStatusDoing 失败: %v", err)
		}

		startAt := ResolveTaskStartTime(Cfg.TimeStart, string(config.SaleStart))
		if startAt != nil && Cfg.TimeStart == nil {
			log.WithFields(logrus.Fields{"username": config.Username, "detail": config.Detail}).
				Infof("使用配置 sale_start 定时: %s", startAt.Format("2006-01-02 15:04:05"))
		}
		buyErr := w.Buy(cancelCtx, config, startAt, Cfg.Interval, Cfg.PushplusToken)

		w.mu.Lock()
		w.cancel = nil
		w.mu.Unlock()

		fields := logrus.Fields{"username": config.Username, "detail": config.Detail}
		if buyErr != nil {
			log.WithFields(fields).Warningf("抢票结束: %v", buyErr)
		}

		// 412 / ctx 取消：CancelTask(Risking)，任务回 Pending，worker 冷却
		if isRiskExit(buyErr) {
			reason := riskReasonFromErr(buyErr)
			if err := w.enterRisking(taskId, reason); err != nil {
				log.WithFields(fields).Warningf("上报 Risking 失败: %v", err)
			} else {
				log.WithFields(fields).Warnf("已因风控进入 Risking reason=%s，任务将重新调度", reason)
			}
			return
		}

		// 正常结束（成功或终端订单态）
		w.m.ClearDetail()
		if err := w.m.UpdateWorkerStatusAndTaskStatus(Idle, TaskStatusDone, taskId); err != nil {
			log.WithFields(fields).Warningf("设置状态 Idle,TaskStatusDone 失败: %v", err)
		}
	}()

	return nil
}

func isRiskExit(err error) bool {
	return err != nil && (errors.Is(err, ErrRiskControl) || errors.Is(err, context.Canceled))
}

func riskReasonFromErr(err error) string {
	if err == nil {
		return "unknown"
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "proxy-exhausted") || strings.Contains(msg, "代理耗尽"):
		return "proxy-exhausted"
	case errors.Is(err, ErrRiskControl):
		return "http-412"
	case errors.Is(err, context.Canceled):
		return "canceled"
	default:
		return "risk"
	}
}

// enterRisking 通知 master：worker 冷却 + 任务回 Pending
func (w *Worker) enterRisking(taskId string, reason string) error {
	if reason == "" {
		reason = "http-412"
	}
	// proxy 使用 Register 已缓存的标签（Buy 中 SetDetail 会更新）
	if err := w.m.CancelTaskWithReason(Risking, reason, ""); err != nil {
		w.m.SetDetail(reason, "")
		_ = w.m.UpdateWorkerStatusAndTaskStatus(Risking, TaskStatusPending, "")
		return err
	}
	return w.m.UpdateWorkerStatusAndTaskStatus(Risking, TaskStatusPending, "")
}
