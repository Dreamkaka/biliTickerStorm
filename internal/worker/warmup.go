package worker

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// Warmup 对齐上游 refresh_hot_and_warm：项目详情复检 + 多连接预热。
func (bc *BiliClient) Warmup(ctx context.Context, projectID int) error {
	n := bc.connPerHost()
	if n < 1 {
		n = 1
	}
	log.Infof("预热/复检：project_id=%d 并发连接=%d proxy=%s", projectID, n, bc.proxyLabel())

	detailURL := projectDetailURL(projectID)
	warmURL := baseURL + "/"

	var okCount int32
	var firstErr error
	var errOnce sync.Once
	var wg sync.WaitGroup

	for i := 0; i < n; i++ {
		wg.Add(1)
		target := warmURL
		if projectID > 0 && i%2 == 1 {
			target = detailURL
		}
		go func(u string) {
			defer wg.Done()
			if err := bc.warmupOne(ctx, u); err != nil {
				errOnce.Do(func() { firstErr = err })
				return
			}
			atomic.AddInt32(&okCount, 1)
		}(target)
	}
	wg.Wait()

	if projectID > 0 {
		if _, err := bc.Get(detailURL); err != nil {
			log.Warnf("预热：项目详情复检失败: %v", err)
			if firstErr == nil {
				firstErr = err
			}
		} else {
			atomic.AddInt32(&okCount, 1)
		}
	}

	log.Infof("预热/复检完成：成功 %d 次（目标并发 %d）", okCount, n)
	if okCount == 0 && firstErr != nil {
		return firstErr
	}
	return nil
}

func (bc *BiliClient) warmupOne(ctx context.Context, rawURL string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	bc.setRequestHeaders(req, "")
	req.Header.Del("Content-Type")
	req.Header.Set("Accept", "*/*")

	if bc.httpClient == nil {
		return fmt.Errorf("http client nil")
	}
	c := *bc.httpClient
	c.Timeout = 8 * time.Second
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	return nil
}

func (bc *BiliClient) connPerHost() int {
	if Cfg != nil && Cfg.ConnPerHost > 0 {
		return Cfg.ConnPerHost
	}
	return defaultConnPerHost
}

func (bc *BiliClient) createBatchSize() int {
	if Cfg != nil && Cfg.CreateBatchSize > 0 {
		return Cfg.CreateBatchSize
	}
	return 1
}

func (bc *BiliClient) warmupEnabled() bool {
	if Cfg == nil {
		return true
	}
	return Cfg.EnableWarmup
}
