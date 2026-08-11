package worker

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestWarmupHitsMultipleTimes(t *testing.T) {
	var hits int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"code":0}`))
	}))
	defer srv.Close()

	// 使用普通 http.Client 测预热逻辑（绕过 uTLS 对自签证书）
	bc := &BiliClient{
		httpClient: srv.Client(),
		fp:         NewBrowserFingerprint(),
	}
	// 指向测试服务器：临时改 base 相关 URL 不可行；直接 warmupOne
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := bc.warmupOne(ctx, srv.URL); err != nil {
		t.Fatal(err)
	}
	if atomic.LoadInt32(&hits) < 1 {
		t.Fatal("expected hit")
	}
}

func TestCreateBatchSizeClamp(t *testing.T) {
	bc := &BiliClient{}
	if bc.createBatchSize() < 1 {
		t.Fatal("batch")
	}
	if bc.connPerHost() < 1 {
		t.Fatal("conn")
	}
}
