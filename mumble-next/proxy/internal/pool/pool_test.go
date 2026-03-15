package pool

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestNewConnPool(t *testing.T) {
	logger := zap.NewNop()
	pool := NewConnPool("localhost", 64738, 10, 100, time.Minute, 10*time.Second, logger)

	if pool == nil {
		t.Fatal("expected pool to be created")
	}

	if pool.host != "localhost" {
		t.Errorf("expected host localhost, got %s", pool.host)
	}

	pool.Close()
}

func TestConnPool_Stats(t *testing.T) {
	logger := zap.NewNop()
	pool := NewConnPool("localhost", 64738, 10, 100, time.Minute, 10*time.Second, logger)
	defer pool.Close()

	stats := pool.Stats()

	if stats.TotalConns != 0 {
		t.Errorf("expected 0 total conns, got %d", stats.TotalConns)
	}

	if stats.ActiveConns != 0 {
		t.Errorf("expected 0 active conns, got %d", stats.ActiveConns)
	}
}

func TestConnPool_GetPut(t *testing.T) {
	// 启动测试服务器
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start test server: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()

	addr := listener.Addr().(*net.TCPAddr)

	logger := zap.NewNop()
	pool := NewConnPool("127.0.0.1", addr.Port, 10, 100, time.Minute, 10*time.Second, logger)

	// 获取连接
	ctx := context.Background()
	conn, err := pool.Get(ctx)
	if err != nil {
		t.Fatalf("failed to get connection: %v", err)
	}

	stats := pool.Stats()
	if stats.ActiveConns != 1 {
		t.Errorf("expected 1 active conn, got %d", stats.ActiveConns)
	}

	// 归还连接
	pool.Put(conn)

	stats = pool.Stats()
	if stats.ActiveConns != 0 {
		t.Errorf("expected 0 active conns after put, got %d", stats.ActiveConns)
	}

	// 关闭
	pool.Close()
	listener.Close()
	wg.Wait()
}

func TestConnPool_Close(t *testing.T) {
	// 启动测试服务器
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start test server: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()

	addr := listener.Addr().(*net.TCPAddr)

	logger := zap.NewNop()
	pool := NewConnPool("127.0.0.1", addr.Port, 10, 100, time.Minute, 10*time.Second, logger)

	// 获取连接
	ctx := context.Background()
	conn, err := pool.Get(ctx)
	if err != nil {
		listener.Close()
		t.Fatalf("failed to get connection: %v", err)
	}

	// 关闭连接池
	pool.Close()
	listener.Close()
	wg.Wait()

	// 尝试归还应该不会 panic
	pool.Put(conn)

	// 尝试从关闭的池获取连接应该失败
	_, err = pool.Get(ctx)
	if err == nil {
		t.Error("expected error when getting from closed pool")
	}
}

func TestConnPool_IdleReuse(t *testing.T) {
	// 启动测试服务器
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start test server: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()

	addr := listener.Addr().(*net.TCPAddr)

	logger := zap.NewNop()
	pool := NewConnPool("127.0.0.1", addr.Port, 10, 100, time.Minute, 10*time.Second, logger)

	ctx := context.Background()

	// 获取并归还连接
	conn1, err := pool.Get(ctx)
	if err != nil {
		t.Fatalf("failed to get connection: %v", err)
	}
	pool.Put(conn1)

	// 再次获取 - 应该复用空闲连接
	conn2, err := pool.Get(ctx)
	if err != nil {
		t.Fatalf("failed to get second connection: %v", err)
	}
	pool.Put(conn2)

	stats := pool.Stats()
	if stats.CreateCount > 1 {
		t.Errorf("expected at most 1 new connection, got %d", stats.CreateCount)
	}

	pool.Close()
	listener.Close()
	wg.Wait()
}

func TestHealthChecker(t *testing.T) {
	// 启动测试服务器
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start test server: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()

	addr := listener.Addr().(*net.TCPAddr)

	logger := zap.NewNop()
	pool := NewConnPool("127.0.0.1", addr.Port, 10, 100, time.Minute, 10*time.Second, logger)

	checker := NewHealthChecker(pool, logger)

	err = checker.Check()
	if err != nil {
		t.Errorf("expected health check to pass, got error: %v", err)
	}

	pool.Close()
	listener.Close()
	wg.Wait()
}

func TestHealthChecker_Unreachable(t *testing.T) {
	logger := zap.NewNop()
	pool := NewConnPool("127.0.0.1", 1, 10, 100, time.Minute, time.Second, logger)
	defer pool.Close()

	checker := NewHealthChecker(pool, logger)

	err := checker.Check()
	if err == nil {
		t.Error("expected health check to fail for unreachable server")
	}
}

func TestConnPool_MaxActiveLimit(t *testing.T) {
	// Start test server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start test server: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()

	addr := listener.Addr().(*net.TCPAddr)

	logger := zap.NewNop()
	// Max active = 2
	pool := NewConnPool("127.0.0.1", addr.Port, 10, 2, time.Minute, 10*time.Second, logger)

	// Get first connection
	ctx := context.Background()
	conn1, err := pool.Get(ctx)
	if err != nil {
		t.Fatalf("failed to get first connection: %v", err)
	}

	// Get second connection
	conn2, err := pool.Get(ctx)
	if err != nil {
		t.Fatalf("failed to get second connection: %v", err)
	}

	// Third connection should wait or fail
	timeoutCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err = pool.Get(timeoutCtx)
	if err == nil {
		t.Error("expected timeout error when max active reached")
	}

	conn1.Close()
	conn2.Close()
	pool.Close()
	listener.Close()
	wg.Wait()
}

func TestConnPool_PutNil(t *testing.T) {
	logger := zap.NewNop()
	pool := NewConnPool("localhost", 64738, 10, 100, time.Minute, 10*time.Second, logger)
	defer pool.Close()

	// Put nil should not panic
	pool.Put(nil)
}

func TestConnPool_GetFromClosedPool(t *testing.T) {
	logger := zap.NewNop()
	pool := NewConnPool("localhost", 64738, 10, 100, time.Minute, 10*time.Second, logger)
	pool.Close()

	ctx := context.Background()
	_, err := pool.Get(ctx)
	if err == nil {
		t.Error("expected error when getting from closed pool")
	}
}

func TestConnPool_PooledConnClose(t *testing.T) {
	// Start test server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start test server: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()

	addr := listener.Addr().(*net.TCPAddr)

	logger := zap.NewNop()
	pool := NewConnPool("127.0.0.1", addr.Port, 10, 100, time.Minute, 10*time.Second, logger)

	ctx := context.Background()
	conn, err := pool.Get(ctx)
	if err != nil {
		t.Fatalf("failed to get connection: %v", err)
	}

	// Close the pooled connection (should return to pool)
	err = conn.Close()
	if err != nil {
		t.Errorf("unexpected error on close: %v", err)
	}

	// Check that connection was returned
	stats := pool.Stats()
	if stats.IdleConns != 1 {
		t.Errorf("expected 1 idle connection, got %d", stats.IdleConns)
	}

	pool.Close()
	listener.Close()
	wg.Wait()
}

func TestConnPool_StaleConnection(t *testing.T) {
	// Start test server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start test server: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()

	addr := listener.Addr().(*net.TCPAddr)

	logger := zap.NewNop()
	// Very short idle timeout
	pool := NewConnPool("127.0.0.1", addr.Port, 10, 100, 10*time.Millisecond, 10*time.Second, logger)

	ctx := context.Background()
	conn, err := pool.Get(ctx)
	if err != nil {
		t.Fatalf("failed to get connection: %v", err)
	}

	// Return to pool
	pool.Put(conn)

	// Wait for idle timeout
	time.Sleep(50 * time.Millisecond)

	// Get again - should create new connection since old one is stale
	conn2, err := pool.Get(ctx)
	if err != nil {
		t.Fatalf("failed to get new connection: %v", err)
	}
	conn2.Close()

	pool.Close()
	listener.Close()
	wg.Wait()
}

func TestConnPool_CleanupStale(t *testing.T) {
	// Start test server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start test server: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			// Keep connection open briefly
			time.Sleep(time.Second)
			conn.Close()
		}
	}()

	addr := listener.Addr().(*net.TCPAddr)

	logger := zap.NewNop()
	// Very short idle timeout
	pool := NewConnPool("127.0.0.1", addr.Port, 10, 100, 50*time.Millisecond, 10*time.Second, logger)

	ctx := context.Background()

	// Create multiple connections and return them
	for i := 0; i < 3; i++ {
		conn, err := pool.Get(ctx)
		if err != nil {
			t.Fatalf("failed to get connection: %v", err)
		}
		pool.Put(conn)
	}

	// Wait for idle timeout
	time.Sleep(100 * time.Millisecond)

	// Manually trigger cleanup
	pool.cleanupStale()

	// Check stats - idle connections should be cleaned up
	stats := pool.Stats()
	t.Logf("Stats: IdleConns=%d, ActiveConns=%d", stats.IdleConns, stats.ActiveConns)

	pool.Close()
	listener.Close()
	wg.Wait()
}

func TestConnPool_StatsWithErrors(t *testing.T) {
	logger := zap.NewNop()
	pool := NewConnPool("127.0.0.1", 1, 10, 100, time.Minute, time.Second, logger)
	defer pool.Close()

	// Try to get connection from unreachable server
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, _ = pool.Get(ctx)

	stats := pool.Stats()
	if stats.CreateErrors == 0 {
		t.Error("expected create errors")
	}
}