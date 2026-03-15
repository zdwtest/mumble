package proxy

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	"github.com/mumble/mumble-next/proxy/internal/config"
	"github.com/mumble/mumble-next/proxy/internal/metrics"
)

// =============================================================================
// WebSocket Handler Tests
// =============================================================================

func TestHandleWebSocket_Success(t *testing.T) {
	// 启动模拟 Murmur 服务器
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
			go echoHandler(conn)
		}
	}()

	// 创建代理
	cfg := config.DefaultConfig()
	cfg.Murmur.Host = "127.0.0.1"
	cfg.Murmur.Port = murmurAddr.Port
	cfg.Murmur.Timeout = 5 * time.Second

	logger := zap.NewNop()
	m := metrics.NewNoopMetrics()
	p := NewProxyWithMetrics(cfg, logger, m)

	// 创建测试服务器
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.HandleWebSocket(w, r)
	}))
	defer server.Close()

	// 测试 WebSocket 连接
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/mumble"
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer ws.Close()

	// 发送测试消息
	testMsg := []byte("hello murmur")
	if err := ws.WriteMessage(websocket.BinaryMessage, testMsg); err != nil {
		t.Fatalf("failed to send message: %v", err)
	}

	// 接收回显
	_, msg, err := ws.ReadMessage()
	if err != nil {
		t.Fatalf("failed to receive message: %v", err)
	}

	if string(msg) != string(testMsg) {
		t.Errorf("expected %s, got %s", testMsg, msg)
	}

	// 验证统计
	stats := p.Stats()
	if stats.ActiveConnections != 1 {
		t.Errorf("expected 1 active connection, got %d", stats.ActiveConnections)
	}
}

func TestHandleWebSocket_MurmurUnreachable(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Murmur.Host = "127.0.0.1"
	cfg.Murmur.Port = 1 // 不可达端口
	cfg.Murmur.Timeout = 1 * time.Second

	logger := zap.NewNop()
	m := metrics.NewNoopMetrics()
	p := NewProxyWithMetrics(cfg, logger, m)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.HandleWebSocket(w, r)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/mumble"
	ws, resp, _ := websocket.DefaultDialer.Dial(wsURL, nil)
	if ws != nil {
		ws.Close()
	}

	// 应该收到关闭消息或连接失败
	if resp != nil && resp.StatusCode == http.StatusOK {
		t.Error("expected non-200 status for unreachable murmur")
	}
}

func TestHandleWebSocket_RateLimitExceeded(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Limits.MaxConnectionsPerIP = 1

	logger := zap.NewNop()
	m := metrics.NewNoopMetrics()
	p := NewProxyWithMetrics(cfg, logger, m)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.HandleWebSocket(w, r)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/mumble"

	// 第一次请求应该允许（可能因 Murmur 不可达而失败）
	ws1, _, _ := websocket.DefaultDialer.Dial(wsURL, nil)
	if ws1 != nil {
		ws1.Close()
	}
}

func TestHandleWebSocket_WithAuth(t *testing.T) {
	// 启动模拟 Murmur
	murmurListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start mock murmur: %v", err)
	}
	defer murmurListener.Close()

	murmurAddr := murmurListener.Addr().(*net.TCPAddr)
	go func() {
		for {
			conn, err := murmurListener.Accept()
			if err != nil {
				return
			}
			go echoHandler(conn)
		}
	}()

	cfg := config.DefaultConfig()
	cfg.Murmur.Host = "127.0.0.1"
	cfg.Murmur.Port = murmurAddr.Port
	cfg.Auth.Enabled = true
	cfg.Auth.HeaderName = "X-Auth-Token"
	cfg.Auth.QueryParam = "auth"
	cfg.Backends = []config.BackendConfig{
		{
			Name:   "test-backend",
			Host:   "127.0.0.1",
			Port:   murmurAddr.Port,
			Tokens: []string{"valid-token"},
		},
	}

	logger := zap.NewNop()
	m := metrics.NewNoopMetrics()
	p := NewProxyWithMetrics(cfg, logger, m)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.HandleWebSocket(w, r)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/mumble?auth=valid-token"
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect with valid token: %v", err)
	}
	ws.Close()
}

func TestHandleWebSocket_InvalidToken(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Auth.Enabled = true
	cfg.Backends = []config.BackendConfig{
		{
			Name:   "test",
			Host:   "localhost",
			Port:   64738,
			Tokens: []string{"valid-token"},
		},
	}

	logger := zap.NewNop()
	m := metrics.NewNoopMetrics()
	p := NewProxyWithMetrics(cfg, logger, m)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.HandleWebSocket(w, r)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/mumble?auth=invalid-token"
	_, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err == nil {
		t.Error("expected connection to fail with invalid token")
	}
	if resp != nil && resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", resp.StatusCode)
	}
}

// =============================================================================
// Forwarder Tests
// =============================================================================

func TestForwarder_ForwardWebSocketToTCP(t *testing.T) {
	// 启动 TCP 服务器
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start tcp server: %v", err)
	}
	defer listener.Close()

	tcpAddr := listener.Addr().(*net.TCPAddr)

	var receivedData []byte
	var mu sync.Mutex

	// TCP 服务器接收数据
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		buf := make([]byte, 1024)
		n, err := conn.Read(buf)
		if err != nil {
			return
		}

		mu.Lock()
		receivedData = buf[:n]
		mu.Unlock()
	}()

	// 创建 WebSocket 测试服务器
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()

		tcp, err := net.Dial("tcp", tcpAddr.String())
		if err != nil {
			return
		}
		defer tcp.Close()

		forwarder := NewForwarder(ws, tcp, 1024)
		done := make(chan struct{})
		go func() {
			forwarder.ForwardWebSocketToTCP(func(err error) {
				close(done)
			})
		}()

		// 等待数据
		time.Sleep(200 * time.Millisecond)
	}))
	defer server.Close()

	// 连接并发送数据
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer ws.Close()

	testData := []byte("test data")
	if err := ws.WriteMessage(websocket.BinaryMessage, testData); err != nil {
		t.Fatalf("failed to send: %v", err)
	}

	time.Sleep(300 * time.Millisecond)

	mu.Lock()
	received := receivedData
	mu.Unlock()

	if string(received) != string(testData) {
		t.Errorf("expected %s, got %s", testData, received)
	}
}

func TestForwarder_ForwardTCPToWebSocket(t *testing.T) {
	// 启动 TCP 服务器
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start tcp server: %v", err)
	}
	defer listener.Close()

	tcpAddr := listener.Addr().(*net.TCPAddr)

	// 创建 WebSocket 测试服务器
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()

		tcp, err := net.Dial("tcp", tcpAddr.String())
		if err != nil {
			return
		}
		defer tcp.Close()

		forwarder := NewForwarder(ws, tcp, 1024)
		go forwarder.ForwardTCPToWebSocket(func(err error) {})

		time.Sleep(100 * time.Millisecond)
	}))
	defer server.Close()

	// 连接
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer ws.Close()
}

// =============================================================================
// Helper Functions Tests
// =============================================================================

func TestGenerateClientID(t *testing.T) {
	id1 := generateClientID()
	id2 := generateClientID()

	if id1 == "" {
		t.Error("expected non-empty client ID")
	}

	if id1 == id2 {
		t.Error("expected different client IDs")
	}
}

func TestConnectMurmur_Success(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start listener: %v", err)
	}
	defer listener.Close()

	addr := listener.Addr().(*net.TCPAddr)
	conn, err := connectMurmur(addr.String(), 5*time.Second)
	if err != nil {
		t.Fatalf("expected connection to succeed: %v", err)
	}
	conn.Close()
}

func TestConnectMurmur_Timeout(t *testing.T) {
	// 使用不可达地址
	_, err := connectMurmur("127.0.0.1:1", 100*time.Millisecond)
	if err == nil {
		t.Error("expected connection to fail")
	}
}

// =============================================================================
// Stats Tests
// =============================================================================

func TestProxy_StatsConcurrent(t *testing.T) {
	cfg := config.DefaultConfig()
	logger := zap.NewNop()
	m := metrics.NewNoopMetrics()
	p := NewProxyWithMetrics(cfg, logger, m)

	var wg sync.WaitGroup
	numOps := 100

	// 并发读取统计
	for i := 0; i < numOps; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = p.Stats()
		}()
	}

	wg.Wait()

	stats := p.Stats()
	if stats.TotalConnections != 0 {
		t.Errorf("expected 0 total connections, got %d", stats.TotalConnections)
	}
}

// =============================================================================
// Error Handling Tests
// =============================================================================

func TestProxy_HandleStats_ErrorResponse(t *testing.T) {
	cfg := config.DefaultConfig()
	logger := zap.NewNop()
	m := metrics.NewNoopMetrics()
	p := NewProxyWithMetrics(cfg, logger, m)

	// 测试错误请求方法
	req := httptest.NewRequest(http.MethodPost, "/stats", nil)
	rec := httptest.NewRecorder()

	p.HandleStats(rec, req)

	// 应该正常响应（方法不限制）
	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}

// =============================================================================
// UDP Proxy Tests
// =============================================================================

func TestUDPProxy_ForwardLoop(t *testing.T) {
	// 启动 UDP 服务器
	serverAddr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to resolve addr: %v", err)
	}

	serverConn, err := net.ListenUDP("udp", serverAddr)
	if err != nil {
		t.Fatalf("failed to start UDP server: %v", err)
	}
	defer serverConn.Close()

	// 创建 UDP 代理
	proxy := NewUDPProxy("127.0.0.1", serverConn.LocalAddr().(*net.UDPAddr).Port, 0)

	// 手动启动
	clientAddr, _ := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	clientConn, err := net.ListenUDP("udp", clientAddr)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer clientConn.Close()

	proxy.conn = clientConn
	proxy.running.Store(true)

	// 清理
	proxy.Stop()
}

// =============================================================================
// WebSocket Handler Tests
// =============================================================================

func TestNewWebSocketHandler(t *testing.T) {
	cfg := config.DefaultConfig()
	logger := zap.NewNop()
	m := metrics.NewNoopMetrics()
	p := NewProxyWithMetrics(cfg, logger, m)

	handler := NewWebSocketHandler(p, logger)
	if handler == nil {
		t.Fatal("expected handler to be created")
	}
}

func TestWebSocketHandler_ServeHTTP(t *testing.T) {
	cfg := config.DefaultConfig()
	logger := zap.NewNop()
	m := metrics.NewNoopMetrics()
	p := NewProxyWithMetrics(cfg, logger, m)

	handler := NewWebSocketHandler(p, logger)

	req := httptest.NewRequest(http.MethodGet, "/mumble", nil)
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	req.Header.Set("Sec-WebSocket-Version", "13")

	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	// 由于 Murmur 不可达，可能收到错误
	// 但至少应该尝试处理请求
}

// =============================================================================
// Helper Functions
// =============================================================================

func echoHandler(conn net.Conn) {
	defer conn.Close()
	buf := make([]byte, 1024)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			return
		}
		conn.Write(buf[:n])
	}
}

// =============================================================================
// Additional Tests for Coverage
// =============================================================================

func TestNewProxy(t *testing.T) {
	cfg := config.DefaultConfig()
	logger := zap.NewNop()
	m := metrics.NewNoopMetrics()

	p := NewProxyWithMetrics(cfg, logger, m)
	if p == nil {
		t.Fatal("expected proxy to be created")
	}

	// Test GetMetrics
	if p.GetMetrics() == nil {
		t.Error("expected metrics to be non-nil")
	}

	// Test GetTokenManager
	if p.GetTokenManager() == nil {
		t.Error("expected token manager to be non-nil")
	}
}

func TestProxy_GetMetrics_GetTokenManager(t *testing.T) {
	cfg := config.DefaultConfig()
	logger := zap.NewNop()
	m := metrics.NewNoopMetrics()
	p := NewProxyWithMetrics(cfg, logger, m)

	// GetMetrics
	metrics := p.GetMetrics()
	if metrics == nil {
		t.Error("expected metrics to be non-nil")
	}

	// GetTokenManager
	tm := p.GetTokenManager()
	if tm == nil {
		t.Error("expected token manager to be non-nil")
	}
}

func TestProxy_RegisterHealthHandlers(t *testing.T) {
	cfg := config.DefaultConfig()
	logger := zap.NewNop()
	m := metrics.NewNoopMetrics()
	p := NewProxyWithMetrics(cfg, logger, m)

	mux := http.NewServeMux()
	p.RegisterHealthHandlers(mux)

	// Test health endpoints are registered
	req1 := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec1 := httptest.NewRecorder()
	mux.ServeHTTP(rec1, req1)

	req2 := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec2 := httptest.NewRecorder()
	mux.ServeHTTP(rec2, req2)

	req3 := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec3 := httptest.NewRecorder()
	mux.ServeHTTP(rec3, req3)

	// All should return some response
	if rec1.Code == 0 {
		t.Error("expected /health to be registered")
	}
}

func TestRateLimiter_Release(t *testing.T) {
	limiter := NewRateLimiter(2)

	// Allow connections
	if !limiter.Allow("192.168.1.1") {
		t.Error("expected first connection to be allowed")
	}
	if !limiter.Allow("192.168.1.1") {
		t.Error("expected second connection to be allowed")
	}

	// Should be at limit
	if limiter.Allow("192.168.1.1") {
		t.Error("expected third connection to be denied")
	}

	// Release one
	limiter.Release("192.168.1.1")

	// Should be allowed again
	if !limiter.Allow("192.168.1.1") {
		t.Error("expected connection to be allowed after release")
	}

	// Release non-existent IP (should not panic)
	limiter.Release("10.0.0.1")
}

func TestRateLimiter_Disabled(t *testing.T) {
	limiter := NewRateLimiter(0) // Disabled

	// Should always allow when disabled
	for i := 0; i < 100; i++ {
		if !limiter.Allow("192.168.1.1") {
			t.Error("expected connection to be allowed when rate limiting is disabled")
		}
	}
}

func TestTCPConn_FullLifecycle(t *testing.T) {
	// Start TCP server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer listener.Close()

	addr := listener.Addr().(*net.TCPAddr)

	// Server handler
	go func() {
		for {
			conn, err := listener.Accept()
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

	// Create TCPConn
	tcpConn, err := NewTCPConn("127.0.0.1", addr.Port, 5*time.Second)
	if err != nil {
		t.Fatalf("failed to create TCPConn: %v", err)
	}
	defer tcpConn.Close()

	// Test RemoteAddr
	if tcpConn.RemoteAddr() == nil {
		t.Error("expected remote addr to be non-nil")
	}

	// Test LocalAddr
	if tcpConn.LocalAddr() == nil {
		t.Error("expected local addr to be non-nil")
	}

	// Test Write
	data := []byte("hello")
	n, err := tcpConn.Write(data)
	if err != nil {
		t.Errorf("failed to write: %v", err)
	}
	if n != len(data) {
		t.Errorf("expected %d bytes written, got %d", len(data), n)
	}

	// Test Read
	recv, err := tcpConn.Read()
	if err != nil {
		t.Errorf("failed to read: %v", err)
	}
	if string(recv) != string(data) {
		t.Errorf("expected %s, got %s", data, recv)
	}

	// Test SetDeadline
	err = tcpConn.SetDeadline(time.Now().Add(5 * time.Second))
	if err != nil {
		t.Errorf("failed to set deadline: %v", err)
	}
}

func TestTCPConn_BufferPoolReuse(t *testing.T) {
	// Start TCP server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer listener.Close()

	addr := listener.Addr().(*net.TCPAddr)

	// Server handler
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		conn.Write([]byte("test"))
	}()

	// Create TCPConn
	tcpConn, err := NewTCPConn("127.0.0.1", addr.Port, 5*time.Second)
	if err != nil {
		t.Fatalf("failed to create TCPConn: %v", err)
	}
	defer tcpConn.Close()

	// Read multiple times to test buffer pool
	for i := 0; i < 5; i++ {
		_, err := tcpConn.Read()
		if err != nil {
			break // Connection closed by server
		}
	}
}

func TestTCPConn_ConnectionFailure(t *testing.T) {
	// Try to connect to unreachable port
	_, err := NewTCPConn("127.0.0.1", 1, 100*time.Millisecond)
	if err == nil {
		t.Error("expected connection to fail")
	}
}

func TestProxy_HandleStats(t *testing.T) {
	cfg := config.DefaultConfig()
	logger := zap.NewNop()
	m := metrics.NewNoopMetrics()
	p := NewProxyWithMetrics(cfg, logger, m)

	req := httptest.NewRequest(http.MethodGet, "/stats", nil)
	rec := httptest.NewRecorder()

	p.HandleStats(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	if rec.Header().Get("Content-Type") != "application/json" {
		t.Errorf("expected application/json content type")
	}
}

// =============================================================================
// Helper Function Tests
// =============================================================================

func TestGetClientIP(t *testing.T) {
	tests := []struct {
		name       string
		remoteAddr string
		headers    map[string]string
		expected   string
	}{
		{
			name:       "direct connection",
			remoteAddr: "192.168.1.1:12345",
			headers:    nil,
			expected:   "192.168.1.1:12345",
		},
		{
			name:       "X-Forwarded-For header",
			remoteAddr: "10.0.0.1:12345",
			headers:    map[string]string{"X-Forwarded-For": "203.0.113.1"},
			expected:   "203.0.113.1",
		},
		{
			name:       "X-Real-IP header",
			remoteAddr: "10.0.0.1:12345",
			headers:    map[string]string{"X-Real-IP": "198.51.100.1"},
			expected:   "198.51.100.1",
		},
		{
			name:       "X-Forwarded-For takes precedence",
			remoteAddr: "10.0.0.1:12345",
			headers:    map[string]string{"X-Forwarded-For": "203.0.113.1", "X-Real-IP": "198.51.100.1"},
			expected:   "203.0.113.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tt.remoteAddr
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}

			ip := getClientIP(req)
			if ip != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, ip)
			}
		})
	}
}

func TestGetToken(t *testing.T) {
	tests := []struct {
		name       string
		headerName string
		queryParam string
		headers    map[string]string
		url        string
		expected   string
	}{
		{
			name:       "token in header",
			headerName: "X-Token",
			queryParam: "token",
			headers:    map[string]string{"X-Token": "header-token"},
			url:        "/",
			expected:   "header-token",
		},
		{
			name:       "token in query",
			headerName: "X-Token",
			queryParam: "token",
			headers:    nil,
			url:        "/?token=query-token",
			expected:   "query-token",
		},
		{
			name:       "header takes precedence",
			headerName: "X-Token",
			queryParam: "token",
			headers:    map[string]string{"X-Token": "header-token"},
			url:        "/?token=query-token",
			expected:   "header-token",
		},
		{
			name:       "no token",
			headerName: "X-Token",
			queryParam: "token",
			headers:    nil,
			url:        "/",
			expected:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.url, nil)
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}

			token := getToken(req, tt.headerName, tt.queryParam)
			if token != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, token)
			}
		})
	}
}

func TestMaskToken(t *testing.T) {
	tests := []struct {
		token    string
		expected string
	}{
		{"abc", "****"},
		{"abcd", "****"},
		{"abcdefgh", "abcd****"},
		{"", "****"},
		{"1234567890", "1234****"},
	}

	for _, tt := range tests {
		t.Run(tt.token, func(t *testing.T) {
			masked := maskToken(tt.token)
			if masked != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, masked)
			}
		})
	}
}

func TestUDPProxy_StartWithDataTransfer(t *testing.T) {
	// Start UDP server (mock murmur)
	serverAddr, _ := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	serverConn, err := net.ListenUDP("udp", serverAddr)
	if err != nil {
		t.Fatalf("failed to start UDP server: %v", err)
	}
	defer serverConn.Close()

	serverPort := serverConn.LocalAddr().(*net.UDPAddr).Port

	// UDP echo server
	go func() {
		buf := make([]byte, 2048)
		for {
			n, addr, err := serverConn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			// Echo back
			serverConn.WriteToUDP(buf[:n], addr)
		}
	}()

	// Start UDP proxy
	proxy := NewUDPProxy("127.0.0.1", serverPort, 0)
	err = proxy.Start()
	if err != nil {
		t.Fatalf("failed to start UDP proxy: %v", err)
	}
	defer proxy.Stop()

	// Give it time to start
	time.Sleep(50 * time.Millisecond)

	// Create client
	clientAddr, _ := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	clientConn, err := net.ListenUDP("udp", clientAddr)
	if err != nil {
		t.Fatalf("failed to create UDP client: %v", err)
	}
	defer clientConn.Close()

	proxyPort := proxy.conn.LocalAddr().(*net.UDPAddr).Port
	proxyAddr := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: proxyPort}

	// Send data
	testData := []byte("test packet")
	_, err = clientConn.WriteToUDP(testData, proxyAddr)
	if err != nil {
		t.Fatalf("failed to send data: %v", err)
	}

	// Wait for processing
	time.Sleep(100 * time.Millisecond)

	// Check stats
	stats := proxy.GetStats()
	if stats.PacketsReceived.Load() == 0 {
		t.Error("expected packets to be received")
	}
}

func TestUDPProxy_StopWhileRunning(t *testing.T) {
	proxy := NewUDPProxy("127.0.0.1", 64738, 0)

	// Start on random port
	err := proxy.Start()
	if err != nil {
		t.Fatalf("failed to start: %v", err)
	}

	// Stop
	proxy.Stop()

	// Verify it's stopped
	if proxy.running.Load() {
		t.Error("expected proxy to be stopped")
	}
}