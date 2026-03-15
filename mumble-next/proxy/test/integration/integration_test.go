package integration

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	"github.com/mumble/mumble-next/proxy/internal/config"
	"github.com/mumble/mumble-next/proxy/internal/metrics"
	"github.com/mumble/mumble-next/proxy/internal/proxy"
)

// TestIntegration_ProxyStartStop 测试代理启动和停止
func TestIntegration_ProxyStartStop(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.Host = "127.0.0.1"
	cfg.Server.Port = 0 // 使用随机端口

	logger := zap.NewNop()
	m := metrics.NewNoopMetrics()

	p := proxy.NewProxyWithMetrics(cfg, logger, m)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- p.Start(ctx)
	}()

	time.Sleep(100 * time.Millisecond)

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer stopCancel()

	if err := p.Stop(stopCtx); err != nil {
		t.Errorf("failed to stop proxy: %v", err)
	}
}

// TestIntegration_WebSocketConnection 测试 WebSocket 连接
func TestIntegration_WebSocketConnection(t *testing.T) {
	// 启动模拟 Murmur
	murmurListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start mock murmur: %v", err)
	}
	defer murmurListener.Close()

	murmurAddr := murmurListener.Addr().(*net.TCPAddr)

	// 启动代理
	cfg := config.DefaultConfig()
	cfg.Murmur.Host = "127.0.0.1"
	cfg.Murmur.Port = murmurAddr.Port

	logger := zap.NewNop()
	m := metrics.NewNoopMetrics()
	p := proxy.NewProxyWithMetrics(cfg, logger, m)

	// 创建测试服务器
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/mumble" {
			ws, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				return
			}
			defer ws.Close()

			// 连接到 Murmur
			tcp, err := net.Dial("tcp", murmurAddr.String())
			if err != nil {
				ws.WriteMessage(websocket.CloseMessage,
					websocket.FormatCloseMessage(1011, "murmur unavailable"))
				return
			}
			defer tcp.Close()

			// 双向转发
			done := make(chan struct{}, 2)

			go func() {
				defer func() { done <- struct{}{} }()
				for {
					_, data, err := ws.ReadMessage()
					if err != nil {
						return
					}
					tcp.Write(data)
				}
			}()

			go func() {
				defer func() { done <- struct{}{} }()
				buf := make([]byte, 1024)
				for {
					n, err := tcp.Read(buf)
					if err != nil {
						return
					}
					ws.WriteMessage(websocket.BinaryMessage, buf[:n])
				}
			}()

			<-done
		} else if r.URL.Path == "/stats" {
			p.HandleStats(w, r)
		}
	}))
	defer server.Close()

	// 测试 WebSocket 连接
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/mumble"
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect websocket: %v", err)
	}
	defer ws.Close()

	// 发送测试消息
	testMsg := []byte("integration test")
	if err := ws.WriteMessage(websocket.BinaryMessage, testMsg); err != nil {
		t.Fatalf("failed to send message: %v", err)
	}

	t.Log("WebSocket connection test passed")
}

// TestIntegration_MetricsEndpoint 测试指标端点
func TestIntegration_MetricsEndpoint(t *testing.T) {
	cfg := config.DefaultConfig()
	logger := zap.NewNop()
	m := metrics.NewNoopMetrics()

	p := proxy.NewProxyWithMetrics(cfg, logger, m)

	// 测试统计端点
	req := httptest.NewRequest(http.MethodGet, "/stats", nil)
	rec := httptest.NewRecorder()

	p.HandleStats(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	if !strings.Contains(rec.Body.String(), "active_connections") {
		t.Errorf("expected stats response to contain 'active_connections'")
	}

	t.Log("Metrics endpoint test passed")
}

// TestIntegration_RateLimiting 测试速率限制
func TestIntegration_RateLimiting(t *testing.T) {
	limiter := proxy.NewRateLimiter(3)

	ip := "192.168.1.100"

	// 前三次应该允许
	for i := 0; i < 3; i++ {
		if !limiter.Allow(ip) {
			t.Errorf("expected connection %d to be allowed", i+1)
		}
	}

	// 第四次应该被拒绝
	if limiter.Allow(ip) {
		t.Error("expected connection 4 to be denied")
	}

	// 释放一个连接
	limiter.Release(ip)

	// 现在应该允许
	if !limiter.Allow(ip) {
		t.Error("expected connection to be allowed after release")
	}

	t.Log("Rate limiting test passed")
}

// TestIntegration_HealthCheck 测试健康检查
func TestIntegration_HealthCheck(t *testing.T) {
	// 启动模拟 Murmur
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start mock murmur: %v", err)
	}
	defer listener.Close()

	addr := listener.Addr().(*net.TCPAddr)

	cfg := config.DefaultConfig()
	cfg.Murmur.Host = "127.0.0.1"
	cfg.Murmur.Port = addr.Port

	logger := zap.NewNop()
	m := metrics.NewNoopMetrics()
	p := proxy.NewProxyWithMetrics(cfg, logger, m)

	// 测试健康检查端点
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	// 注册健康检查处理器
	mux := http.NewServeMux()
	p.RegisterHealthHandlers(mux)

	// 调用健康检查
	handler, _ := mux.Handler(req)
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	t.Log("Health check test passed")
}

// TestIntegration_MultipleBackends 测试多后端配置
func TestIntegration_MultipleBackends(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Auth.Enabled = true
	cfg.Backends = []config.BackendConfig{
		{
			Name:   "backend1",
			Host:   "backend1.example.com",
			Port:   64738,
			Tokens: []string{"token1", "token2"},
		},
		{
			Name:   "backend2",
			Host:   "backend2.example.com",
			Port:   64738,
			Tokens: []string{"token3"},
		},
	}

	logger := zap.NewNop()
	p := proxy.NewProxy(cfg, logger)

	tm := p.GetTokenManager()

	// 验证 token 路由
	backend, ok := tm.Validate("token1")
	if !ok {
		t.Fatal("expected token1 to be valid")
	}
	if backend.Name != "backend1" {
		t.Errorf("expected backend1, got %s", backend.Name)
	}

	backend, ok = tm.Validate("token3")
	if !ok {
		t.Fatal("expected token3 to be valid")
	}
	if backend.Name != "backend2" {
		t.Errorf("expected backend2, got %s", backend.Name)
	}

	_, ok = tm.Validate("invalid-token")
	if ok {
		t.Error("expected invalid token to be rejected")
	}

	t.Log("Multiple backends test passed")
}

// TestIntegration_ConcurrentConnections 测试并发连接
func TestIntegration_ConcurrentConnections(t *testing.T) {
	// 启动模拟 Murmur
	murmurListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start mock murmur: %v", err)
	}
	defer murmurListener.Close()

	murmurAddr := murmurListener.Addr().(*net.TCPAddr)

	// 接受连接
	go func() {
		for {
			conn, err := murmurListener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 1024)
				for {
					n, err := c.Read(buf)
					if err != nil {
						return
					}
					c.Write(buf[:n])
				}
			}(conn)
		}
	}()

	// 创建 WebSocket 测试服务器
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()

		tcp, err := net.Dial("tcp", murmurAddr.String())
		if err != nil {
			return
		}
		defer tcp.Close()

		done := make(chan struct{}, 2)

		go func() {
			defer func() { done <- struct{}{} }()
			for {
				_, data, err := ws.ReadMessage()
				if err != nil {
					return
				}
				tcp.Write(data)
			}
		}()

		go func() {
			defer func() { done <- struct{}{} }()
			buf := make([]byte, 1024)
			for {
				n, err := tcp.Read(buf)
				if err != nil {
					return
				}
				ws.WriteMessage(websocket.BinaryMessage, buf[:n])
			}
		}()

		<-done
	}))
	defer server.Close()

	// 并发连接测试
	numClients := 10
	clients := make([]*websocket.Conn, numClients)
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	for i := 0; i < numClients; i++ {
		ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			t.Fatalf("client %d failed to connect: %v", i, err)
		}
		clients[i] = ws
	}

	// 发送消息
	for i, ws := range clients {
		msg := fmt.Sprintf("message from client %d", i)
		if err := ws.WriteMessage(websocket.BinaryMessage, []byte(msg)); err != nil {
			t.Errorf("client %d failed to send: %v", i, err)
		}
	}

	// 关闭连接
	for _, ws := range clients {
		ws.Close()
	}

	t.Logf("Concurrent connections test passed with %d clients", numClients)
}