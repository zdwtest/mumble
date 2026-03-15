package drain

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestNewManager(t *testing.T) {
	logger := zap.NewNop()
	manager := NewManager(30*time.Second, logger)

	if manager == nil {
		t.Fatal("expected manager to be created")
	}

	if manager.GetState() != StateNormal {
		t.Errorf("expected initial state to be normal, got %s", manager.GetState())
	}
}

func TestManager_Connections(t *testing.T) {
	logger := zap.NewNop()
	manager := NewManager(30*time.Second, logger)

	// 注册连接
	info := manager.RegisterConnection("conn1", "backend1", "192.168.1.1:12345")

	if info == nil {
		t.Fatal("expected connection info to be returned")
	}

	if manager.GetConnectionCount() != 1 {
		t.Errorf("expected 1 connection, got %d", manager.GetConnectionCount())
	}

	// 注销连接
	manager.UnregisterConnection("conn1")

	if manager.GetConnectionCount() != 0 {
		t.Errorf("expected 0 connections after unregister, got %d", manager.GetConnectionCount())
	}
}

func TestManager_StartDrain_NoConnections(t *testing.T) {
	logger := zap.NewNop()
	manager := NewManager(5*time.Second, logger)

	ctx := context.Background()
	err := manager.StartDrain(ctx)

	if err != nil {
		t.Errorf("expected drain to succeed with no connections, got: %v", err)
	}

	if manager.GetState() != StateTerminating {
		t.Errorf("expected state to be terminating, got %s", manager.GetState())
	}
}

func TestManager_StartDrain_AlreadyDraining(t *testing.T) {
	logger := zap.NewNop()
	manager := NewManager(30*time.Second, logger)

	// 设置为排空状态
	manager.state.Store(int32(StateDraining))

	ctx := context.Background()
	err := manager.StartDrain(ctx)

	if err == nil {
		t.Error("expected error when already draining")
	}
}

func TestManager_DrainWithConnections(t *testing.T) {
	logger := zap.NewNop()
	manager := NewManager(5*time.Second, logger)

	// 注册连接
	manager.RegisterConnection("conn1", "backend1", "192.168.1.1:12345")

	// 开始排空
	done := make(chan error, 1)
	go func() {
		done <- manager.StartDrain(context.Background())
	}()

	// 等待一小段时间后注销连接
	time.Sleep(200 * time.Millisecond)
	manager.UnregisterConnection("conn1")

	// 等待排空完成
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("expected drain to succeed, got: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Error("drain did not complete in time")
	}
}

func TestManager_DrainTimeout(t *testing.T) {
	logger := zap.NewNop()
	manager := NewManager(200*time.Millisecond, logger)

	// 注册连接但不注销
	manager.RegisterConnection("conn1", "backend1", "192.168.1.1:12345")

	// 开始排空
	err := manager.StartDrain(context.Background())

	if err == nil {
		t.Error("expected timeout error")
	}

	if manager.GetState() != StateTerminating {
		t.Errorf("expected state to be terminating after timeout, got %s", manager.GetState())
	}
}

func TestManager_ListConnections(t *testing.T) {
	logger := zap.NewNop()
	manager := NewManager(30*time.Second, logger)

	// 注册多个连接
	manager.RegisterConnection("conn1", "backend1", "192.168.1.1:12345")
	manager.RegisterConnection("conn2", "backend2", "192.168.1.2:12345")

	connections := manager.ListConnections()

	if len(connections) != 2 {
		t.Errorf("expected 2 connections, got %d", len(connections))
	}
}

func TestManager_Callbacks(t *testing.T) {
	logger := zap.NewNop()
	manager := NewManager(100*time.Millisecond, logger)

	startCalled := false
	doneCalled := false

	var mu sync.Mutex
	manager.OnDrainStart(func() {
		mu.Lock()
		startCalled = true
		mu.Unlock()
	})

	manager.OnDrainDone(func() {
		mu.Lock()
		doneCalled = true
		mu.Unlock()
	})

	// 没有连接，排空应该立即完成
	manager.StartDrain(context.Background())

	mu.Lock()
	start := startCalled
	done := doneCalled
	mu.Unlock()

	if !start {
		t.Error("expected onDrainStart to be called")
	}

	if !done {
		t.Error("expected onDrainDone to be called")
	}
}

func TestManager_GetStatus(t *testing.T) {
	logger := zap.NewNop()
	manager := NewManager(30*time.Second, logger)

	manager.RegisterConnection("conn1", "backend1", "192.168.1.1:12345")

	status := manager.GetStatus()

	if status.State != StateNormal {
		t.Errorf("expected state normal, got %s", status.State)
	}

	if status.ConnectionCount != 1 {
		t.Errorf("expected 1 connection, got %d", status.ConnectionCount)
	}
}

func TestHandler_Get(t *testing.T) {
	logger := zap.NewNop()
	manager := NewManager(30*time.Second, logger)
	handler := NewHandler(manager)

	req := httptest.NewRequest(http.MethodGet, "/drain", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}

func TestHandler_Post(t *testing.T) {
	logger := zap.NewNop()
	manager := NewManager(5*time.Second, logger)
	handler := NewHandler(manager)

	req := httptest.NewRequest(http.MethodPost, "/drain", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}

func TestHandler_Post_AlreadyDraining(t *testing.T) {
	logger := zap.NewNop()
	manager := NewManager(30*time.Second, logger)
	manager.state.Store(int32(StateDraining))
	handler := NewHandler(manager)

	req := httptest.NewRequest(http.MethodPost, "/drain", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Errorf("expected status 409, got %d", rec.Code)
	}
}

func TestState_String(t *testing.T) {
	tests := []struct {
		state    State
		expected string
	}{
		{StateNormal, "normal"},
		{StateDraining, "draining"},
		{StateTerminating, "terminating"},
		{State(99), "unknown"},
	}

	for _, tt := range tests {
		if got := tt.state.String(); got != tt.expected {
			t.Errorf("expected %s, got %s", tt.expected, got)
		}
	}
}

func TestManager_ConcurrentConnections(t *testing.T) {
	logger := zap.NewNop()
	manager := NewManager(30*time.Second, logger)

	var wg sync.WaitGroup
	numConns := 100

	// 并发注册连接
	for i := 0; i < numConns; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			connID := fmt.Sprintf("conn%d", id)
			manager.RegisterConnection(connID, "backend1", "192.168.1.1:12345")
		}(i)
	}

	wg.Wait()

	if manager.GetConnectionCount() != int64(numConns) {
		t.Errorf("expected %d connections, got %d", numConns, manager.GetConnectionCount())
	}

	// 并发注销连接
	for i := 0; i < numConns; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			connID := fmt.Sprintf("conn%d", id)
			manager.UnregisterConnection(connID)
		}(i)
	}

	wg.Wait()

	if manager.GetConnectionCount() != 0 {
		t.Errorf("expected 0 connections after unregister, got %d", manager.GetConnectionCount())
	}
}