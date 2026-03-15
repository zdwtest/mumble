package proxy

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

func TestHeartbeatConfig_Default(t *testing.T) {
	config := DefaultHeartbeatConfig()

	if config.PingInterval != 30*time.Second {
		t.Errorf("expected ping interval 30s, got %v", config.PingInterval)
	}

	if config.PongWait != 10*time.Second {
		t.Errorf("expected pong wait 10s, got %v", config.PongWait)
	}

	if config.MaxMissedPongs != 3 {
		t.Errorf("expected max missed pongs 3, got %d", config.MaxMissedPongs)
	}
}

func TestHeartbeatManager_StartStop(t *testing.T) {
	// Create test WebSocket server
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()

		// Handle incoming messages
		for {
			_, _, err := ws.ReadMessage()
			if err != nil {
				return
			}
		}
	}))
	defer server.Close()

	// Connect as client
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer ws.Close()

	// Create heartbeat manager with short interval for testing
	config := HeartbeatConfig{
		PingInterval:   100 * time.Millisecond,
		PongWait:       50 * time.Millisecond,
		MaxMissedPongs: 3,
	}

	logger := zap.NewNop()
	hm := NewHeartbeatManager(ws, config, logger)

	// Start and stop
	hm.Start()
	time.Sleep(150 * time.Millisecond)
	hm.Stop()

	// Verify stats
	pings, _ := hm.Stats()
	if pings == 0 {
		t.Error("expected at least one ping to be sent")
	}
}

func TestKeepAlive_StartStop(t *testing.T) {
	// Create test WebSocket server
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()

		// Respond to pings
		ws.SetPingHandler(func(appData string) error {
			return ws.WriteMessage(websocket.PongMessage, nil)
		})

		for {
			_, _, err := ws.ReadMessage()
			if err != nil {
				return
			}
		}
	}))
	defer server.Close()

	// Connect as client
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer ws.Close()

	// Create keep-alive
	logger := zap.NewNop()
	ka := NewKeepAlive(ws, 100*time.Millisecond, 500*time.Millisecond, logger)

	ka.Start()
	time.Sleep(150 * time.Millisecond)

	// Should be alive
	if !ka.IsAlive() {
		t.Error("expected connection to be alive")
	}

	ka.Stop()
}

func TestRetryConfig_Default(t *testing.T) {
	config := DefaultRetryConfig()

	if config.MaxAttempts != 3 {
		t.Errorf("expected max attempts 3, got %d", config.MaxAttempts)
	}

	if config.InitialDelay != 100*time.Millisecond {
		t.Errorf("expected initial delay 100ms, got %v", config.InitialDelay)
	}

	if config.Multiplier != 2.0 {
		t.Errorf("expected multiplier 2.0, got %f", config.Multiplier)
	}
}

func TestRetryableDialer_Success(t *testing.T) {
	// Start test server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer listener.Close()

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()

	addr := listener.Addr().String()
	logger := zap.NewNop()
	dialer := NewRetryableDialer(DefaultRetryConfig(), logger)

	conn, err := dialer.DialWithRetry(nil, "tcp", addr, 5*time.Second)
	if err != nil {
		t.Errorf("expected connection to succeed: %v", err)
	}
	if conn != nil {
		conn.Close()
	}
}

func TestRetryableDialer_Failure(t *testing.T) {
	logger := zap.NewNop()
	config := RetryConfig{
		MaxAttempts:  2,
		InitialDelay: 10 * time.Millisecond,
		MaxDelay:     50 * time.Millisecond,
		Multiplier:   2.0,
	}
	dialer := NewRetryableDialer(config, logger)

	// Try to connect to unreachable port
	_, err := dialer.DialWithRetry(nil, "tcp", "127.0.0.1:1", 10*time.Millisecond)
	if err == nil {
		t.Error("expected connection to fail")
	}
}

func TestExponentialBackoff(t *testing.T) {
	backoff := NewExponentialBackoff(100*time.Millisecond, 1*time.Second, 2.0)

	// First delay should be initial
	d1 := backoff.Next()
	if d1 != 100*time.Millisecond {
		t.Errorf("expected 100ms, got %v", d1)
	}

	// Second delay should be doubled
	d2 := backoff.Next()
	if d2 != 200*time.Millisecond {
		t.Errorf("expected 200ms, got %v", d2)
	}

	// Third delay should be 400ms
	d3 := backoff.Next()
	if d3 != 400*time.Millisecond {
		t.Errorf("expected 400ms, got %v", d3)
	}

	// Reset and verify
	backoff.Reset()
	d4 := backoff.Next()
	if d4 != 100*time.Millisecond {
		t.Errorf("expected 100ms after reset, got %v", d4)
	}
}

func TestExponentialBackoff_MaxDelay(t *testing.T) {
	backoff := NewExponentialBackoff(500*time.Millisecond, 1*time.Second, 2.0)

	// Should cap at max
	d1 := backoff.Next() // 500ms
	d2 := backoff.Next() // 1s (capped)
	d3 := backoff.Next() // still 1s (capped)

	if d1 != 500*time.Millisecond {
		t.Errorf("expected 500ms, got %v", d1)
	}

	if d2 != 1*time.Second {
		t.Errorf("expected 1s, got %v", d2)
	}

	if d3 != 1*time.Second {
		t.Errorf("expected 1s (capped), got %v", d3)
	}
}

func TestCircuitBreaker_Allow(t *testing.T) {
	cb := NewCircuitBreaker(3, 100*time.Millisecond)

	// Should allow when closed
	if !cb.Allow() {
		t.Error("expected to allow when closed")
	}

	// Record failures
	cb.RecordFailure()
	cb.RecordFailure()

	if !cb.Allow() {
		t.Error("expected to allow after 2 failures")
	}

	// Third failure should open circuit
	cb.RecordFailure()

	if cb.Allow() {
		t.Error("expected to deny when open")
	}

	// Wait for timeout
	time.Sleep(150 * time.Millisecond)

	// Should allow after timeout (half-open)
	if !cb.Allow() {
		t.Error("expected to allow after timeout")
	}
}

func TestCircuitBreaker_RecordSuccess(t *testing.T) {
	cb := NewCircuitBreaker(2, 100*time.Millisecond)

	// Open circuit
	cb.RecordFailure()
	cb.RecordFailure()

	if cb.State() != StateOpen {
		t.Error("expected circuit to be open")
	}

	// Success should close circuit
	cb.RecordSuccess()

	if cb.State() != StateClosed {
		t.Error("expected circuit to be closed after success")
	}
}

func TestHeartbeatManager_Timeout(t *testing.T) {
	// Create test WebSocket server that doesn't respond to pings
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()

		// Don't respond to pings - just read
		for {
			_, _, err := ws.ReadMessage()
			if err != nil {
				return
			}
		}
	}))
	defer server.Close()

	// Connect as client
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer ws.Close()

	// Create heartbeat manager with very aggressive settings
	config := HeartbeatConfig{
		PingInterval:   50 * time.Millisecond,
		PongWait:       10 * time.Millisecond,
		MaxMissedPongs: 2,
	}

	logger := zap.NewNop()
	hm := NewHeartbeatManager(ws, config, logger)

	timeoutCalled := false
	hm.OnTimeout(func() {
		timeoutCalled = true
	})

	hm.Start()

	// Wait for timeout to trigger
	time.Sleep(200 * time.Millisecond)

	hm.Stop()

	if !timeoutCalled {
		t.Error("expected timeout callback to be called")
	}
}

func TestKeepAlive_Timeout(t *testing.T) {
	// Create test WebSocket server that doesn't respond to pings
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()

		// Don't respond to pings
		for {
			_, _, err := ws.ReadMessage()
			if err != nil {
				return
			}
		}
	}))
	defer server.Close()

	// Connect as client
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer ws.Close()

	// Create keep-alive with very short timeout
	logger := zap.NewNop()
	ka := NewKeepAlive(ws, 50*time.Millisecond, 100*time.Millisecond, logger)

	ka.Start()
	time.Sleep(200 * time.Millisecond)

	// Should not be alive
	if ka.IsAlive() {
		t.Error("expected connection to not be alive after timeout")
	}

	ka.Stop()
}

func TestRetryableDialer_RetryableErrors(t *testing.T) {
	logger := zap.NewNop()
	dialer := NewRetryableDialer(DefaultRetryConfig(), logger)

	// Test isRetryableError with timeout error
	timeoutErr := &timeoutError{}
	if !dialer.isRetryableError(timeoutErr) {
		t.Error("expected timeout error to be retryable")
	}
}

// timeoutError implements net.Error for testing
type timeoutError struct{}

func (e *timeoutError) Error() string   { return "timeout" }
func (e *timeoutError) Timeout() bool   { return true }
func (e *timeoutError) Temporary() bool { return true }

func TestCircuitBreaker_HalfOpen(t *testing.T) {
	cb := NewCircuitBreaker(2, 50*time.Millisecond)

	// Open circuit
	cb.RecordFailure()
	cb.RecordFailure()

	if cb.State() != StateOpen {
		t.Error("expected circuit to be open")
	}

	// Wait for timeout
	time.Sleep(100 * time.Millisecond)

	// Should transition to half-open on Allow
	if !cb.Allow() {
		t.Error("expected Allow to return true after timeout")
	}

	// Record failure in half-open state
	cb.RecordFailure()

	// Should be open again
	if cb.State() != StateOpen {
		t.Error("expected circuit to be open after failure in half-open")
	}
}