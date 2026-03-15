package drain

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

// State 排空状态
type State int32

const (
	StateNormal State = iota
	StateDraining
	StateTerminating
)

// String 返回状态字符串
func (s State) String() string {
	switch s {
	case StateNormal:
		return "normal"
	case StateDraining:
		return "draining"
	case StateTerminating:
		return "terminating"
	default:
		return "unknown"
	}
}

// Manager 排空管理器
type Manager struct {
	state        atomic.Int32
	connections  sync.Map // connection ID -> *ConnectionInfo
	connCount    atomic.Int64
	drainTimeout time.Duration
	logger       *zap.Logger

	// 通知通道
	drainStartCh chan struct{}
	drainDoneCh  chan struct{}

	// 回调
	onDrainStart func()
	onDrainDone  func()
}

// ConnectionInfo 连接信息
type ConnectionInfo struct {
	ID         string
	StartTime  time.Time
	Backend    string
	RemoteAddr string
}

// NewManager 创建排空管理器
func NewManager(drainTimeout time.Duration, logger *zap.Logger) *Manager {
	return &Manager{
		drainTimeout: drainTimeout,
		logger:       logger,
		drainStartCh: make(chan struct{}),
		drainDoneCh:  make(chan struct{}),
	}
}

// RegisterConnection 注册连接
func (m *Manager) RegisterConnection(id, backend, remoteAddr string) *ConnectionInfo {
	info := &ConnectionInfo{
		ID:         id,
		StartTime:  time.Now(),
		Backend:    backend,
		RemoteAddr: remoteAddr,
	}

	m.connections.Store(id, info)
	m.connCount.Add(1)

	m.logger.Debug("connection registered",
		zap.String("id", id),
		zap.String("backend", backend),
	)

	return info
}

// UnregisterConnection 注销连接
func (m *Manager) UnregisterConnection(id string) {
	m.connections.Delete(id)
	count := m.connCount.Add(-1)

	m.logger.Debug("connection unregistered",
		zap.String("id", id),
		zap.Int64("remaining", count),
	)

	// 如果正在排空且没有连接了，通知完成
	if m.GetState() == StateDraining && count == 0 {
		m.notifyDrainDone()
	}
}

func (m *Manager) notifyDrainDone() {
	select {
	case <-m.drainDoneCh:
		// 已经关闭
	default:
		close(m.drainDoneCh)
	}
}

// GetConnectionCount 获取连接数
func (m *Manager) GetConnectionCount() int64 {
	return m.connCount.Load()
}

// GetState 获取状态
func (m *Manager) GetState() State {
	return State(m.state.Load())
}

// StartDrain 开始排空
func (m *Manager) StartDrain(ctx context.Context) error {
	// 设置状态
	if !m.state.CompareAndSwap(int32(StateNormal), int32(StateDraining)) {
		return fmt.Errorf("already in state: %s", m.GetState())
	}

	connCount := m.connCount.Load()

	m.logger.Info("starting drain",
		zap.Int64("connections", connCount),
		zap.Duration("timeout", m.drainTimeout),
	)

	// 通知开始
	close(m.drainStartCh)

	// 调用回调
	if m.onDrainStart != nil {
		m.onDrainStart()
	}

	// 如果没有连接，立即完成
	if connCount == 0 {
		m.logger.Info("drain completed immediately (no connections)")
		m.state.Store(int32(StateTerminating))
		if m.onDrainDone != nil {
			m.onDrainDone()
		}
		return nil
	}

	// 等待所有连接完成或超时
	drainCtx, cancel := context.WithTimeout(ctx, m.drainTimeout)
	defer cancel()

	select {
	case <-drainCtx.Done():
		remaining := m.connCount.Load()
		m.logger.Warn("drain timeout reached",
			zap.Int64("remaining_connections", remaining),
		)
		m.state.Store(int32(StateTerminating))
		return fmt.Errorf("drain timeout with %d connections remaining", remaining)
	case <-m.drainDoneCh:
		m.logger.Info("drain completed successfully")
		m.state.Store(int32(StateTerminating))

		// 调用回调
		if m.onDrainDone != nil {
			m.onDrainDone()
		}
		return nil
	}
}

// WaitForDrainStart 等待排空开始
func (m *Manager) WaitForDrainStart() <-chan struct{} {
	return m.drainStartCh
}

// IsDraining 是否正在排空
func (m *Manager) IsDraining() bool {
	return m.GetState() == StateDraining
}

// OnDrainStart 设置排空开始回调
func (m *Manager) OnDrainStart(fn func()) {
	m.onDrainStart = fn
}

// OnDrainDone 设置排空完成回调
func (m *Manager) OnDrainDone(fn func()) {
	m.onDrainDone = fn
}

// ListConnections 列出所有连接
func (m *Manager) ListConnections() []ConnectionInfo {
	var connections []ConnectionInfo
	m.connections.Range(func(key, value any) bool {
		if info, ok := value.(*ConnectionInfo); ok {
			connections = append(connections, *info)
		}
		return true
	})
	return connections
}

// DrainStatus 排空状态信息
type DrainStatus struct {
	State           State         `json:"state"`
	ConnectionCount int64         `json:"connection_count"`
	DrainTimeout    time.Duration `json:"drain_timeout"`
	Uptime          time.Duration `json:"uptime"`
}

// GetStatus 获取状态信息
func (m *Manager) GetStatus() DrainStatus {
	return DrainStatus{
		State:           m.GetState(),
		ConnectionCount: m.connCount.Load(),
		DrainTimeout:    m.drainTimeout,
	}
}

// Handler HTTP 处理器
type Handler struct {
	manager *Manager
}

// NewHandler 创建处理器
func NewHandler(manager *Manager) *Handler {
	return &Handler{manager: manager}
}

// ServeHTTP 实现 http.Handler
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	status := h.manager.GetStatus()

	switch r.Method {
	case http.MethodGet:
		// 返回状态
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"state":"%s","connections":%d,"timeout":"%s"}`,
			status.State, status.ConnectionCount, status.DrainTimeout)
	case http.MethodPost:
		// 开始排空
		if h.manager.IsDraining() {
			http.Error(w, "already draining", http.StatusConflict)
			return
		}

		go h.manager.StartDrain(r.Context())

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"state":"%s","message":"drain started"}`, StateDraining)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}