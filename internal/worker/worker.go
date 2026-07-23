package worker

import (
	. "biliTickerStorm/internal/common"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

		buyErr := w.Buy(cancelCtx, config, Cfg.TimeStart, Cfg.Interval, Cfg.PushplusToken)

		w.mu.Lock()
		w.cancel = nil
		w.mu.Unlock()

		fields := logrus.Fields{"username": config.Username, "detail": config.Detail}
		if buyErr != nil {
			log.WithFields(fields).Warningf("抢票结束: %v", buyErr)
		}

		// 412 / ctx 取消：已（或应）经 CancelTask(Risking)，任务回 Pending，worker 冷却
		if isRiskExit(buyErr) {
			if err := w.enterRisking(taskId); err != nil {
				log.WithFields(fields).Warningf("上报 Risking 失败: %v", err)
			} else {
				log.WithFields(fields).Warn("已因风控进入 Risking，任务将重新调度")
			}
			return
		}

		// 正常结束（成功或终端订单态）
		if err := w.m.UpdateWorkerStatusAndTaskStatus(Idle, TaskStatusDone, taskId); err != nil {
			log.WithFields(fields).Warningf("设置状态 Idle,TaskStatusDone 失败: %v", err)
		}
	}()

	return nil
}

func isRiskExit(err error) bool {
	return err != nil && (errors.Is(err, ErrRiskControl) || errors.Is(err, context.Canceled))
}

// enterRisking 通知 master：worker 冷却 + 任务回 Pending
func (w *Worker) enterRisking(taskId string) error {
	// 先 RPC CancelTask（master 侧清任务并 Ban）
	if err := w.m.CancelTask(Risking); err != nil {
		// 兜底：本地标 Risking，清任务绑定，避免心跳把任务标 Done
		_ = w.m.UpdateWorkerStatusAndTaskStatus(Risking, TaskStatusPending, "")
		return err
	}
	// 与 master 对齐本地状态；TaskAssigned 置空，防止后续心跳误写 Done
	return w.m.UpdateWorkerStatusAndTaskStatus(Risking, TaskStatusPending, "")
}
