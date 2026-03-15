package proxy

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestReconnectConfig_Default(t *testing.T) {
	cfg := DefaultReconnectConfig()

	if !cfg.Enabled {
		t.Error("expected reconnect to be enabled by default")
	}
	if cfg.MaxAttempts != 10 {
		t.Errorf("expected max attempts 10, got %d", cfg.MaxAttempts)
	}
	if cfg.InitialDelay != 100*time.Millisecond {
		t.Errorf("expected initial delay 100ms, got %v", cfg.InitialDelay)
	}
}

func TestReconnector_Connect_Success(t *testing.T) {
	logger := zap.NewNop()
	cfg := DefaultReconnectConfig()
	r := NewReconnector(cfg, logger)
	defer r.Stop()

	callCount := 0
	err := r.Connect(context.Background(), func(ctx context.Context) error {
		callCount++
		return nil
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if callCount != 1 {
		t.Errorf("expected 1 dial call, got %d", callCount)
	}
	if r.GetState() != StateConnected {
		t.Errorf("expected state Connected, got %v", r.GetState())
	}
}

func TestReconnector_Connect_Retry(t *testing.T) {
	logger := zap.NewNop()
	cfg := DefaultReconnectConfig()
	cfg.InitialDelay = 10 * time.Millisecond
	cfg.MaxDelay = 50 * time.Millisecond
	r := NewReconnector(cfg, logger)
	defer r.Stop()

	attempts := 0
	err := r.Connect(context.Background(), func(ctx context.Context) error {
		attempts++
		if attempts < 3 {
			return errors.New("connection failed")
		}
		return nil
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
}

func TestReconnector_Connect_MaxAttempts(t *testing.T) {
	logger := zap.NewNop()
	cfg := DefaultReconnectConfig()
	cfg.MaxAttempts = 2
	cfg.InitialDelay = 10 * time.Millisecond
	r := NewReconnector(cfg, logger)
	defer r.Stop()

	attempts := 0
	err := r.Connect(context.Background(), func(ctx context.Context) error {
		attempts++
		return errors.New("always fails")
	})

	if err == nil {
		t.Error("expected error after max attempts")
	}
	if attempts != 2 {
		t.Errorf("expected 2 attempts, got %d", attempts)
	}
}

func TestReconnector_Connect_Cancel(t *testing.T) {
	logger := zap.NewNop()
	cfg := DefaultReconnectConfig()
	cfg.InitialDelay = time.Second // Long delay
	r := NewReconnector(cfg, logger)
	defer r.Stop()

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	err := r.Connect(ctx, func(ctx context.Context) error {
		return errors.New("always fails")
	})

	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestReconnector_Stop(t *testing.T) {
	logger := zap.NewNop()
	cfg := DefaultReconnectConfig()
	cfg.InitialDelay = time.Second
	r := NewReconnector(cfg, logger)

	go func() {
		time.Sleep(50 * time.Millisecond)
		r.Stop()
	}()

	err := r.Connect(context.Background(), func(ctx context.Context) error {
		return errors.New("always fails")
	})

	if err == nil {
		t.Error("expected error after stop")
	}
	if r.GetState() != StateStopped {
		t.Errorf("expected state Stopped, got %v", r.GetState())
	}
}

func TestReconnector_Callbacks(t *testing.T) {
	logger := zap.NewNop()
	cfg := DefaultReconnectConfig()
	cfg.InitialDelay = 10 * time.Millisecond

	connectCalled := false
	reconnectCalled := false
	reconnectAttempts := 0

	cfg.OnConnect = func() {
		connectCalled = true
	}
	cfg.OnReconnect = func(attempt int, delay time.Duration) {
		reconnectCalled = true
		reconnectAttempts++
	}

	r := NewReconnector(cfg, logger)
	defer r.Stop()

	attempts := 0
	_ = r.Connect(context.Background(), func(ctx context.Context) error {
		attempts++
		if attempts < 2 {
			return errors.New("fail first")
		}
		return nil
	})

	if !connectCalled {
		t.Error("expected OnConnect to be called")
	}
	if !reconnectCalled {
		t.Error("expected OnReconnect to be called")
	}
}

func TestReconnector_Reset(t *testing.T) {
	logger := zap.NewNop()
	cfg := DefaultReconnectConfig()
	r := NewReconnector(cfg, logger)

	// Simulate some attempts
	r.incrementAttempt()
	r.incrementAttempt()

	if r.GetAttempt() != 2 {
		t.Errorf("expected attempt 2, got %d", r.GetAttempt())
	}

	r.Reset()

	if r.GetAttempt() != 0 {
		t.Errorf("expected attempt 0 after reset, got %d", r.GetAttempt())
	}
}

func TestReconnector_Disabled(t *testing.T) {
	logger := zap.NewNop()
	cfg := DefaultReconnectConfig()
	cfg.Enabled = false
	r := NewReconnector(cfg, logger)

	err := r.Connect(context.Background(), func(ctx context.Context) error {
		return errors.New("fail")
	})

	if err == nil {
		t.Error("expected error when disabled")
	}
}

func TestConnectionState_Connected(t *testing.T) {
	s := NewConnectionState()

	s.SetConnected()

	if !s.IsConnected() {
		t.Error("expected to be connected")
	}
	if s.GetState() != StateConnected {
		t.Errorf("expected state Connected, got %v", s.GetState())
	}
	if s.GetConnectedAt().IsZero() {
		t.Error("expected connected time to be set")
	}
}

func TestConnectionState_Disconnected(t *testing.T) {
	s := NewConnectionState()

	s.SetConnected()
	s.SetDisconnected(errors.New("test error"))

	if s.IsConnected() {
		t.Error("expected to be disconnected")
	}
	if s.GetLastError() == nil {
		t.Error("expected error to be set")
	}
}

func TestConnectionState_Reconnecting(t *testing.T) {
	s := NewConnectionState()

	s.SetReconnecting()

	if s.GetState() != StateReconnecting {
		t.Errorf("expected state Reconnecting, got %v", s.GetState())
	}
}

func TestConnectionState_Uptime(t *testing.T) {
	s := NewConnectionState()

	// Not connected
	if s.Uptime() != 0 {
		t.Error("expected 0 uptime when not connected")
	}

	s.SetConnected()
	time.Sleep(50 * time.Millisecond)

	uptime := s.Uptime()
	if uptime < 50*time.Millisecond {
		t.Errorf("expected uptime >= 50ms, got %v", uptime)
	}

	s.SetDisconnected(nil)
	if s.Uptime() != 0 {
		t.Error("expected 0 uptime after disconnect")
	}
}

func TestConnectionState_Concurrent(t *testing.T) {
	s := NewConnectionState()

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.SetConnected()
			_ = s.IsConnected()
			_ = s.GetState()
			_ = s.Uptime()
			s.SetDisconnected(nil)
		}()
	}

	wg.Wait()
}

func TestReconnectState_String(t *testing.T) {
	tests := []struct {
		state    ReconnectState
		expected int
	}{
		{StateDisconnected, 0},
		{StateConnected, 1},
		{StateReconnecting, 2},
		{StateStopped, 3},
	}

	for _, tt := range tests {
		if int(tt.state) != tt.expected {
			t.Errorf("state value mismatch: got %d, expected %d", tt.state, tt.expected)
		}
	}
}

func BenchmarkReconnector_Connect(b *testing.B) {
	logger := zap.NewNop()
	cfg := DefaultReconnectConfig()
	cfg.InitialDelay = time.Millisecond

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r := NewReconnector(cfg, logger)
		_ = r.Connect(context.Background(), func(ctx context.Context) error {
			return nil
		})
		r.Stop()
	}
}