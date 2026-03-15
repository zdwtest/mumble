package proxy

import (
	"sync"
	"testing"
)

func TestRateLimiter_GetCount(t *testing.T) {
	limiter := NewRateLimiter(10)

	// 初始计数应为 0
	if count := limiter.GetCount("192.168.1.1"); count != 0 {
		t.Errorf("expected initial count 0, got %d", count)
	}

	// 添加几个连接
	limiter.Allow("192.168.1.1")
	limiter.Allow("192.168.1.1")
	limiter.Allow("192.168.1.1")

	if count := limiter.GetCount("192.168.1.1"); count != 3 {
		t.Errorf("expected count 3, got %d", count)
	}

	// 不同 IP 的计数应独立
	limiter.Allow("192.168.1.2")
	limiter.Allow("192.168.1.2")

	if count := limiter.GetCount("192.168.1.2"); count != 2 {
		t.Errorf("expected count 2 for different IP, got %d", count)
	}
}

func TestTCPConn_BufferPool(t *testing.T) {
	// 测试 buffer pool 逻辑
	pool := &sync.Pool{
		New: func() interface{} {
			return make([]byte, 32*1024)
		},
	}

	buf := pool.Get().([]byte)
	if len(buf) != 32*1024 {
		t.Errorf("expected buffer size 32768, got %d", len(buf))
	}

	pool.Put(buf)
}