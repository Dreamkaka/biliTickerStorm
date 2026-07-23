package worker

import (
	. "biliTickerStorm/internal/common"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/sirupsen/logrus"
)

var log = GetLogger("worker")

func (w *Worker) Buy(ctx context.Context, ticketsInfo BiliTickerBuyConfig, timeStart *time.Time, interval int, pushplusToken string) error {
	log.WithFields(logrus.Fields{
		"detail":        ticketsInfo.Detail,
		"timeStart":     timeStart,
		"interval":      interval,
		"pushplusToken": pushplusToken,
		"Username":      ticketsInfo.Username,
	}).Info("接受到抢票任务")
	client := NewBiliClient(ticketsInfo.Cookies, w)

	if timeStart != nil {
		log.Infof("开始时间 :%s", timeStart.String())
		if err := SleepUntilAccurate(*timeStart); err != nil {
			return err
		}
	}

	for {
		select {
		case <-ctx.Done():
			if err := w.m.CancelTask(Risking); err != nil {
				return err
			}
			return fmt.Errorf("任务被取消: %w", ctx.Err())
		default:
		}

		ticketCollectionT := time.Now().UnixMilli()
		href := fmt.Sprintf("https://mall.bilibili.com/neul-next/ticket-renovation/detail.html?id=%d", ticketsInfo.ProjectId)
		ticketState := InitCTokenState(GenerateBrowserWindowState(), 0, len(defaultUserAgent), len(href), ticketCollectionT)

		log.Info("1）订单准备")
		prepareSnap := ticketState.Snapshot(ticketCollectionT)
		preparePayload := ticketsInfo.BuildPreparePayload(prepareSnap.GeneratePrepareCToken())
		prepareURL := fmt.Sprintf("%s/api/ticket/order/prepare?project_id=%d", baseURL, ticketsInfo.ProjectId)
		resp, err := client.Post(prepareURL, preparePayload)
		if err != nil {
			if errors.Is(err, ErrRateLimit) {
				log.Warnf("准备订单触发 429，延迟 %dms 后重试", defaultRateLimitMs)
				time.Sleep(time.Duration(defaultRateLimitMs) * time.Millisecond)
			} else if errors.Is(err, ErrRiskControl) {
				return fmt.Errorf("任务被取消: %w", err)
			} else {
				log.Errorf("准备订单请求失败: %v", err)
			}
			continue
		}
		var requestResult map[string]interface{}
		if err := json.Unmarshal(resp, &requestResult); err != nil {
			log.Errorf("准备订单解析失败: %s", summarizeNonJSON(resp))
			continue
		}
		code := getIntFromMap(requestResult, "errno", "code")
		log.Infof("订单准备结果: errno=%d msg=%s", code, errnoMessage(code))
		if code == -401 || code == 100044 {
			log.Info("检测到验证码，调用验证码服务处理")
			if err := HandleCaptcha(client, requestResult, ticketsInfo.Phone); err != nil {
				log.Warnf("验证码失败: %v", err)
			} else {
				log.Info("验证码通过")
			}
			continue
		}
		orderToken := extractPrepareToken(requestResult)
		if orderToken == "" {
			log.Info("重新准备订单，原因：订单准备未返回有效 token")
			continue
		}
		ticketsInfo.Token = orderToken
		ptoken := extractPreparePToken(requestResult)

		log.Info("2）创建订单")
		var errno int = -1
		tokenExpired := false
		terminalStop := false
		for attempt := 1; attempt <= defaultCreateRetry; attempt++ {
			select {
			case <-ctx.Done():
				if err := w.m.CancelTask(Risking); err != nil {
					return err
				}
				return fmt.Errorf("任务被取消: %w", ctx.Err())
			default:
			}

			nowMs := time.Now().UnixMilli()
			createSnap := SimCTokenState(ticketState, nowMs)
			body, err := ticketsInfo.ToCreateV2RequestBody(orderToken, createSnap.GenerateCreateCToken(), ptoken, nowMs)
			if err != nil {
				log.Errorf("[尝试 %d/%d] 创建CreateV2请求体失败: %v", attempt, defaultCreateRetry, err)
				time.Sleep(time.Duration(interval) * time.Millisecond)
				continue
			}
			createURL := fmt.Sprintf("%s/api/ticket/order/createV2?project_id=%d", baseURL, ticketsInfo.ProjectId)
			if ptoken != "" {
				createURL += "&ptoken=" + ptoken
			}
			resp, err := client.Post(createURL, body)
			if err != nil {
				if errors.Is(err, ErrRateLimit) {
					log.Warnf("[尝试 %d/%d] 429 限流，延迟 %dms", attempt, defaultCreateRetry, defaultRateLimitMs)
					time.Sleep(time.Duration(defaultRateLimitMs) * time.Millisecond)
					continue
				}
				if errors.Is(err, ErrRiskControl) {
					return fmt.Errorf("任务被取消: %w", err)
				}
				log.Errorf("[尝试 %d/%d] 请求异常: %v", attempt, defaultCreateRetry, err)
				time.Sleep(time.Duration(interval) * time.Millisecond)
				continue
			}
			var ret map[string]interface{}
			if err := json.Unmarshal(resp, &ret); err != nil {
				log.Errorf("[尝试 %d/%d] 解析响应失败: %s", attempt, defaultCreateRetry, summarizeNonJSON(resp))
				time.Sleep(time.Duration(interval) * time.Millisecond)
				continue
			}
			errno = getIntFromMap(ret, "errno", "code")
			errMsg := errnoMessage(errno)
			respMsg := getStringFromMap(ret, "msg", "message")
			if respMsg != "" {
				log.Infof("[Create] attempt=%d errno=%d msg=%s | %s", attempt, errno, errMsg, respMsg)
			} else {
				log.Infof("[Create] attempt=%d errno=%d msg=%s", attempt, errno, errMsg)
			}

			if isCreateSuccess(ret, errno) {
				log.Info("3）抢票成功，请前往订单中心查看")
				if pushplusToken != "" {
					_ = sendPushPlusMessage(pushplusToken, "抢票成功", "前往订单中心付款吧")
				}
				return nil
			}
			if errno == 100034 {
				if data, ok := ret["data"].(map[string]interface{}); ok {
					if payMoney, ok := data["pay_money"].(float64); ok {
						log.Infof("更新票价为：%.2f", payMoney/100)
						ticketsInfo.PayMoney = int(payMoney)
					}
				}
			}
			if errno == 100003 {
				log.Info("该项目每人限购1张，已存在购买订单，停止重试")
				terminalStop = true
				break
			}
			if errno == 100048 || errno == 100079 {
				log.Info("已经下单，有尚未完成订单")
				terminalStop = true
				break
			}
			if errno == 100051 {
				log.Info("token过期，需要重新准备订单")
				tokenExpired = true
				break
			}
			time.Sleep(time.Duration(interval) * time.Millisecond)
		}

		if terminalStop {
			return nil
		}
		if tokenExpired || errno == 100051 {
			continue
		}
		log.Info("0）重新下单")
	}
}

func summarizeNonJSON(body []byte) string {
	s := string(body)
	s = strings.ReplaceAll(s, "\r", "\\r")
	s = strings.ReplaceAll(s, "\n", "\\n")
	if len(s) > 300 {
		s = s[:300] + "..."
	}
	if s == "" {
		return "<empty>"
	}
	return s
}
