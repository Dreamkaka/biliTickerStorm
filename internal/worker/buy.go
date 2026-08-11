package worker

import (
	. "biliTickerStorm/internal/common"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
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

	// 对齐上游：开售前/启动时预热连接 + 项目详情复检
	if client.warmupEnabled() {
		wctx, wcancel := context.WithTimeout(ctx, 15*time.Second)
		if err := client.Warmup(wctx, ticketsInfo.ProjectId); err != nil {
			log.Warnf("启动预热失败（忽略）: %v", err)
		}
		wcancel()
	}

	rateLimitMs := defaultRateLimitMs
	riskLocalRetries := 5
	riskBaseMs := 1000
	riskMaxSec := 30
	if Cfg != nil {
		if Cfg.RateLimitDelayMs > 0 {
			rateLimitMs = Cfg.RateLimitDelayMs
		}
		if Cfg.RiskLocalRetries > 0 {
			riskLocalRetries = Cfg.RiskLocalRetries
		}
		if Cfg.RiskCooldownBaseMs > 0 {
			riskBaseMs = Cfg.RiskCooldownBaseMs
		}
		if Cfg.RiskCooldownMaxSec > 0 {
			riskMaxSec = Cfg.RiskCooldownMaxSec
		}
	}
	riskHits := 0
	batchSize := client.createBatchSize()

	for {
		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.Canceled) {
				return fmt.Errorf("任务被取消: %w", ErrRiskControl)
			}
			return fmt.Errorf("任务被取消: %w", ctx.Err())
		default:
		}

		ticketCollectionT := time.Now().UnixMilli()
		href := mobileDetailHref(ticketsInfo.ProjectId)
		// user_agent_length 与实际请求头 UA 一致（指纹会话内固定）
		fp := client.Fingerprint()
		ticketState := InitCTokenState(GenerateBrowserWindowState(), 0, fp.UserAgentLength(), len(href), ticketCollectionT)

		log.Info("1）订单准备")
		prepareSnap := ticketState.Snapshot(ticketCollectionT)
		preparePayload := ticketsInfo.BuildPreparePayload(prepareSnap.GeneratePrepareCToken())
		prepareURL := fmt.Sprintf("%s/api/ticket/order/prepare?project_id=%d", baseURL, ticketsInfo.ProjectId)
		resp, err := client.Post(prepareURL, preparePayload)
		if err != nil {
			if cont, retErr := handleTransportError(ctx, client, &riskHits, riskLocalRetries, riskBaseMs, riskMaxSec, rateLimitMs, err, "准备订单"); retErr != nil {
				return retErr
			} else if cont {
				continue
			}
			continue
		}
		client.markProxySuccess()
		if w.m != nil {
			w.m.SetDetail("", client.proxyLabel())
		}
		var requestResult map[string]interface{}
		if err := json.Unmarshal(resp, &requestResult); err != nil {
			// 对齐上游：非 JSON 且诊断含 412 时走代理失败路径
			if isNonJSON412(resp) {
				log.Warnf("准备订单非 JSON 含 412: %s", summarizeNonJSON(resp))
				if cont, retErr := handleRisk412(ctx, client, &riskHits, riskLocalRetries, riskBaseMs, riskMaxSec); retErr != nil {
					return retErr
				} else if cont {
					continue
				}
				continue
			}
			log.Errorf("准备订单解析失败: %s", summarizeNonJSON(resp))
			continue
		}
		code := getIntFromMap(requestResult, "errno", "code")
		log.Infof("订单准备结果: errno=%d msg=%s", code, errnoMessage(code))
		if code == -401 || code == errnoCaptcha {
			log.Warn("prepare 返回人机验证/风险态，继续重试（已移除极验依赖）")
			if w.m != nil {
				w.m.SetDetail("captcha-risk", client.proxyLabel())
			}
			time.Sleep(time.Duration(interval) * time.Millisecond)
			continue
		}
		orderToken := extractPrepareToken(requestResult)
		if orderToken == "" {
			log.Info("重新准备订单，原因：订单准备未返回有效 token")
			continue
		}
		ticketsInfo.Token = orderToken
		ptoken := extractPreparePToken(requestResult)
		riskHits = 0

		log.Info("2）创建订单")
		var errno int = -1
		tokenExpired := false
		terminalStop := false
		createURL := fmt.Sprintf("%s/api/ticket/order/createV2?project_id=%d", baseURL, ticketsInfo.ProjectId)
		if ptoken != "" {
			createURL += "&ptoken=" + ptoken
		}

		for attempt := 1; attempt <= defaultCreateRetry; {
			select {
			case <-ctx.Done():
				if errors.Is(ctx.Err(), context.Canceled) {
					return fmt.Errorf("任务被取消: %w", ErrRiskControl)
				}
				return fmt.Errorf("任务被取消: %w", ctx.Err())
			default:
			}

			batchEnd := attempt + batchSize - 1
			if batchEnd > defaultCreateRetry {
				batchEnd = defaultCreateRetry
			}
			n := batchEnd - attempt + 1
			results := make([]createAttemptResult, n)
			var wg sync.WaitGroup
			for i := 0; i < n; i++ {
				wg.Add(1)
				go func(i, att int) {
					defer wg.Done()
					results[i] = doCreateAttempt(client, &ticketsInfo, ticketState, orderToken, ptoken, createURL, att)
				}(i, attempt+i)
			}
			wg.Wait()

			// 按 attempt 顺序处理业务结果；传输错误取第一条
			var transportErr error
			needSleep := true
			for i := 0; i < n; i++ {
				r := results[i]
				if r.err != nil {
					if transportErr == nil {
						transportErr = r.err
					}
					continue
				}
				client.markProxySuccess()
				errno = r.errno
				errMsg := errnoMessage(errno)
				if r.respMsg != "" {
					log.Infof("[Create] attempt=%d errno=%d msg=%s | %s", r.attempt, errno, errMsg, r.respMsg)
				} else {
					log.Infof("[Create] attempt=%d errno=%d msg=%s", r.attempt, errno, errMsg)
				}
				switch classifyCreateErrno(errno, r.ret) {
				case CreateSuccess:
					log.Info("3）抢票成功，请前往订单中心查看")
					detail := ticketsInfo.Detail
					if detail == "" {
						detail = fmt.Sprintf("project_id=%d", ticketsInfo.ProjectId)
					}
					content := fmt.Sprintf("bilibili会员购抢票成功，请尽快付款。%s", detail)
					// 兼容仅传入 pushplusToken 参数的旧调用；优先用 env 全渠道
					if pushplusToken != "" && (Cfg == nil || Cfg.PushplusToken == "") {
						_ = sendPushPlusMessage(pushplusToken, "抢票成功", content)
					}
					NotifyAll("抢票成功", content, 15*time.Second)
					return nil
				case CreateUpdatePayMoney:
					if pay, ok := extractPayMoney(r.ret); ok {
						log.Infof("更新票价为：%.2f", float64(pay)/100)
						ticketsInfo.PayMoney = pay
					}
				case CreateTerminal:
					logTerminal(errno, r.ret)
					terminalStop = true
				case CreateTokenExpired:
					log.Info("token过期，需要重新准备订单")
					tokenExpired = true
				case CreateProjectRefresh:
					log.Info("100001：项目详情复检 + 连接预热")
					if client.warmupEnabled() {
						wctx, wcancel := context.WithTimeout(ctx, 12*time.Second)
						if err := client.Warmup(wctx, ticketsInfo.ProjectId); err != nil {
							log.Warnf("100001 预热失败: %v", err)
						}
						wcancel()
					} else if err := refreshProjectDetail(client, ticketsInfo.ProjectId); err != nil {
						log.Warnf("项目详情复检失败: %v", err)
					}
				case CreateCaptchaRisk:
					log.Warn("create 返回人机验证风险态，继续重试")
					if w.m != nil {
						w.m.SetDetail("captcha-risk", client.proxyLabel())
					}
				}
				if terminalStop || tokenExpired {
					break
				}
			}

			if transportErr != nil && !terminalStop && !tokenExpired {
				if cont, retErr := handleTransportError(ctx, client, &riskHits, riskLocalRetries, riskBaseMs, riskMaxSec, rateLimitMs, transportErr, fmt.Sprintf("创建订单[%d]", attempt)); retErr != nil {
					return retErr
				} else if cont {
					needSleep = false
				}
			}

			attempt = batchEnd + 1
			if terminalStop || tokenExpired {
				break
			}
			if needSleep {
				time.Sleep(time.Duration(interval) * time.Millisecond)
			}
		}

		if terminalStop {
			return nil
		}
		if tokenExpired || errno == errnoTokenExpired {
			continue
		}
		log.Info("0）重新下单")
	}
}

type createAttemptResult struct {
	attempt int
	errno   int
	ret     map[string]interface{}
	respMsg string
	err     error
}

func doCreateAttempt(client *BiliClient, ticketsInfo *BiliTickerBuyConfig, ticketState *CTokenRuntimeState, orderToken, ptoken, createURL string, attempt int) createAttemptResult {
	nowMs := time.Now().UnixMilli()
	createSnap := SimCTokenState(ticketState, nowMs)
	body, err := ticketsInfo.ToCreateV2RequestBody(orderToken, createSnap.GenerateCreateCToken(), ptoken, nowMs)
	if err != nil {
		return createAttemptResult{attempt: attempt, err: err}
	}
	resp, err := client.Post(createURL, body)
	if err != nil {
		return createAttemptResult{attempt: attempt, err: err}
	}
	var ret map[string]interface{}
	if err := json.Unmarshal(resp, &ret); err != nil {
		if isNonJSON412(resp) {
			return createAttemptResult{attempt: attempt, err: ErrRiskControl}
		}
		return createAttemptResult{attempt: attempt, err: fmt.Errorf("parse: %s", summarizeNonJSON(resp))}
	}
	errno := getIntFromMap(ret, "errno", "code")
	return createAttemptResult{
		attempt: attempt,
		errno:   errno,
		ret:     ret,
		respMsg: getStringFromMap(ret, "msg", "message"),
	}
}

func logTerminal(errno int, ret map[string]interface{}) {
	switch errno {
	case errnoDuplicateBuy:
		log.Info("该项目每人限购1张，已存在购买订单，停止重试")
	case errnoPendingOrder:
		if orderURL := extractOrderURL(ret); orderURL != "" {
			log.Infof("有尚未完成订单，停止重试: %s", orderURL)
		} else {
			log.Info("有尚未完成订单，停止重试")
		}
	case errnoDupOrder:
		log.Info("有重复订单，停止重试")
	default:
		log.Info("终端订单态，停止重试")
	}
}

// handleTransportError 对齐上游 BiliRateLimitError / 412 / RequestException。
func handleTransportError(ctx context.Context, client *BiliClient, riskHits *int, maxRetries, baseMs, maxSec, rateLimitMs int, err error, stage string) (cont bool, retErr error) {
	if errors.Is(err, ErrRateLimit) {
		log.Warnf("%s 触发 429，延迟 %dms 后继续", stage, rateLimitMs)
		if rateLimitMs > 0 {
			time.Sleep(time.Duration(rateLimitMs) * time.Millisecond)
		}
		return true, nil
	}
	if errors.Is(err, ErrRiskControl) {
		return handleRisk412(ctx, client, riskHits, maxRetries, baseMs, maxSec)
	}
	// 对齐上游 RequestException → handle_proxy_failure（切代理 / 冷却）
	log.Errorf("%s 请求异常: %v", stage, err)
	return handleProxyOrCooldown(ctx, client, riskHits, maxRetries, baseMs, maxSec, "network:"+stage)
}

// handleRisk412：优先切代理；无可用代理则本地冷却；耗尽则返回 ErrRiskControl。
func handleRisk412(ctx context.Context, client *BiliClient, riskHits *int, maxRetries, baseMs, maxSec int) (cont bool, err error) {
	return handleProxyOrCooldown(ctx, client, riskHits, maxRetries, baseMs, maxSec, "http-412")
}

// handleProxyOrCooldown 对齐上游 handle_proxy_failure：标记失败 → 切换 → API 补池 → 递增冷却。
// 集群差异：本地恢复次数耗尽后上报 Risking（单机 Buy 则无限 backoff）。
func handleProxyOrCooldown(ctx context.Context, client *BiliClient, riskHits *int, maxRetries, baseMs, maxSec int, reason string) (cont bool, err error) {
	if client.worker != nil && client.worker.m != nil {
		client.worker.m.SetDetail(reason, client.proxyLabel())
	}
	switched, exhausted := client.handleProxyRisk(reason)
	if client.worker != nil && client.worker.m != nil {
		client.worker.m.SetDetail(reason, client.proxyLabel())
	}
	if switched {
		*riskHits = 0
		log.Infof("%s 已切换代理，立即重试", reason)
		return true, nil
	}
	*riskHits++
	if *riskHits > maxRetries {
		out := reason
		if exhausted || strings.Contains(reason, "proxy") {
			out = "proxy-exhausted"
			log.Warnf("代理耗尽且本地恢复失败（%d 次），上报集群风控", maxRetries)
		} else {
			log.Warnf("%s 本地恢复耗尽（%d 次），上报集群风控", reason, maxRetries)
		}
		if client.worker != nil && client.worker.m != nil {
			client.worker.m.SetDetail(out, client.proxyLabel())
		}
		return false, fmt.Errorf("%s: %w", out, ErrRiskControl)
	}
	wait := riskCooldown(*riskHits, baseMs, maxSec)
	log.Warnf("%s 本地冷却 %s 后继续 (%d/%d) proxy=%s", reason, wait, *riskHits, maxRetries, client.proxyLabel())
	if err := sleepCtx(ctx, wait); err != nil {
		return false, err
	}
	return true, nil
}

// isNonJSON412 对齐上游 summarize_non_json_response 中 status=412 判定。
func isNonJSON412(body []byte) bool {
	s := string(body)
	if strings.Contains(s, "status=412") || strings.Contains(s, "412 风控") {
		return true
	}
	lower := strings.ToLower(s)
	return strings.Contains(s, "412") && strings.Contains(lower, "precondition")
}

func refreshProjectDetail(client *BiliClient, projectID int) error {
	_, err := client.Get(projectDetailURL(projectID))
	return err
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		if errors.Is(ctx.Err(), context.Canceled) {
			return fmt.Errorf("任务被取消: %w", ErrRiskControl)
		}
		return fmt.Errorf("任务被取消: %w", ctx.Err())
	case <-t.C:
		return nil
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
