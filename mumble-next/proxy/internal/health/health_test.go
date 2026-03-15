package health

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewChecker(t *testing.T) {
	checker := NewChecker("localhost", 64738)
	if checker == nil {
		t.Fatal("expected checker to be created")
	}

	if checker.murmurHost != "localhost" {
		t.Errorf("expected murmurHost to be localhost, got %s", checker.murmurHost)
	}

	if checker.murmurPort != 64738 {
		t.Errorf("expected murmurPort to be 64738, got %d", checker.murmurPort)
	}
}

func TestChecker_CheckMurmur_Reachable(t *testing.T) {
	// 启动一个测试 TCP 服务器
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start test server: %v", err)
	}
	defer listener.Close()

	// 获取实际端口
	addr := listener.Addr().(*net.TCPAddr)
	checker := NewChecker("127.0.0.1", addr.Port)

	check := checker.CheckMurmur()

	if check.Status != StatusHealthy {
		t.Errorf("expected status to be healthy, got %s, error: %s", check.Status, check.Error)
	}

	// Latency can be 0 on very fast local connections
	if check.Latency < 0 {
		t.Errorf("expected non-negative latency, got %d", check.Latency)
	}
}

func TestChecker_CheckMurmur_Unreachable(t *testing.T) {
	// 使用一个不可能连接的端口
	checker := NewChecker("127.0.0.1", 1) // Port 1 通常不可用

	check := checker.CheckMurmur()

	if check.Status != StatusUnhealthy {
		t.Errorf("expected status to be unhealthy, got %s", check.Status)
	}

	if check.Error == "" {
		t.Error("expected error message for unreachable server")
	}
}

func TestChecker_Check(t *testing.T) {
	// 启动测试服务器
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start test server: %v", err)
	}
	defer listener.Close()

	addr := listener.Addr().(*net.TCPAddr)
	checker := NewChecker("127.0.0.1", addr.Port)

	status := checker.Check()

	if status == nil {
		t.Fatal("expected status to be returned")
	}

	if status.Status != StatusHealthy {
		t.Errorf("expected overall status to be healthy, got %s", status.Status)
	}

	if status.Checks == nil {
		t.Fatal("expected checks map to be populated")
	}

	murmurCheck, exists := status.Checks["murmur"]
	if !exists {
		t.Fatal("expected murmur check to exist")
	}

	if murmurCheck.Status != StatusHealthy {
		t.Errorf("expected murmur check to be healthy, got %s", murmurCheck.Status)
	}

	if status.Uptime == "" {
		t.Error("expected uptime to be set")
	}
}

func TestChecker_Handler(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start test server: %v", err)
	}
	defer listener.Close()

	addr := listener.Addr().(*net.TCPAddr)
	checker := NewChecker("127.0.0.1", addr.Port)

	handler := checker.Handler()

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	var status HealthStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if status.Status != StatusHealthy {
		t.Errorf("expected healthy status, got %s", status.Status)
	}
}

func TestChecker_Handler_Unhealthy(t *testing.T) {
	checker := NewChecker("127.0.0.1", 1) // Unreachable port

	handler := checker.Handler()

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status 503, got %d", rec.Code)
	}

	var status HealthStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if status.Status != StatusUnhealthy {
		t.Errorf("expected unhealthy status, got %s", status.Status)
	}
}

func TestChecker_LivenessHandler(t *testing.T) {
	checker := NewChecker("localhost", 64738)

	handler := checker.LivenessHandler()

	req := httptest.NewRequest(http.MethodGet, "/live", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	if rec.Body.String() != "OK" {
		t.Errorf("expected body 'OK', got %s", rec.Body.String())
	}
}

func TestChecker_ReadinessHandler_Ready(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start test server: %v", err)
	}
	defer listener.Close()

	addr := listener.Addr().(*net.TCPAddr)
	checker := NewChecker("127.0.0.1", addr.Port)

	handler := checker.ReadinessHandler()

	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}

func TestChecker_ReadinessHandler_NotReady(t *testing.T) {
	checker := NewChecker("127.0.0.1", 1)

	handler := checker.ReadinessHandler()

	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status 503, got %d", rec.Code)
	}
}

func TestChecker_GetLastStatus(t *testing.T) {
	checker := NewChecker("localhost", 64738)

	// 初始状态应为 nil
	if checker.GetLastStatus() != nil {
		t.Error("expected initial last status to be nil")
	}

	// 执行检查后应保存状态
	checker.Check()

	if checker.GetLastStatus() == nil {
		t.Error("expected last status to be set after check")
	}
}

func TestHealthStatus_Uptime(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start test server: %v", err)
	}
	defer listener.Close()

	addr := listener.Addr().(*net.TCPAddr)
	checker := NewChecker("127.0.0.1", addr.Port)

	time.Sleep(100 * time.Millisecond) // 等待一点时间

	status := checker.Check()

	// 验证 uptime 已设置且大于 0
	if status.Uptime == "" {
		t.Error("expected uptime to be set")
	}

	// uptime 应该大于 100ms
	if checker.startTime.After(time.Now().Add(-200 * time.Millisecond)) {
		// 开始时间在 200ms 内，这是正确的
	} else {
		t.Error("unexpected start time")
	}
}