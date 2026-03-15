package proxy

import (
	"context"
	"errors"
	"math/rand"
	"sync"
	"time"

	"go.uber.org/zap"
)

// ReconnectConfig 重连配置
type ReconnectConfig struct {
	// Enabled 是否启用自动重连
	Enabled bool

	// MaxAttempts 最大重连次数 (0 表示无限)
	MaxAttempts int

	// InitialDelay 初始延迟
	InitialDelay time.Duration

	// MaxDelay 最大延迟
	MaxDelay time.Duration

	// Multiplier 延迟倍数
	Multiplier float64

	// Jitter 添加随机抖动
	Jitter bool

	// OnConnect 连接成功回调
	OnConnect func()

	// OnDisconnect 断开连接回调
	OnDisconnect func(error)

	// OnReconnect 重连回调
	OnReconnect func(attempt int, delay time.Duration)
}

// DefaultReconnectConfig 默认重连配置
func DefaultReconnectConfig() ReconnectConfig {
	return ReconnectConfig{
		Enabled:      true,
		MaxAttempts:  10,
		InitialDelay: 100 * time.Millisecond,
		MaxDelay:     30 * time.Second,
		Multiplier:   2.0,
		Jitter:       true,
	}
}

// Reconnector 自动重连器
type Reconnector struct {
	config    ReconnectConfig
	logger    *zap.Logger
	attempt   int
	state     ReconnectState
	mu        sync.RWMutex
	cancelCtx context.CancelFunc
	stopChan  chan struct{}
}

// ReconnectState 重连状态
type ReconnectState int

const (
	// StateDisconnected 已断开
	StateDisconnected ReconnectState = iota
	// StateConnected 已连接
	StateConnected
	// StateReconnecting 重连中
	StateReconnecting
	// StateStopped 已停止
	StateStopped
)

// NewReconnector 创建重连器
func NewReconnector(config ReconnectConfig, logger *zap.Logger) *Reconnector {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Reconnector{
		config:   config,
		logger:   logger,
		state:    StateDisconnected,
		stopChan: make(chan struct{}),
	}
}

// Connect 执行连接（带重连逻辑）
func (r *Reconnector) Connect(ctx context.Context, dialFunc func(context.Context) error) error {
	if !r.config.Enabled {
		return dialFunc(ctx)
	}

	for {
		attempt := r.getAttempt()
		if r.config.MaxAttempts > 0 && attempt > r.config.MaxAttempts {
			return errors.New("max reconnection attempts reached")
		}

		r.setState(StateReconnecting)

		err := dialFunc(ctx)
		if err == nil {
			r.resetAttempt()
			r.setState(StateConnected)
			if r.config.OnConnect != nil {
				r.config.OnConnect()
			}
			return nil
		}

		r.logger.Debug("connection failed",
			zap.Int("attempt", attempt),
			zap.Error(err),
		)

		if r.config.OnDisconnect != nil {
			r.config.OnDisconnect(err)
		}

		// 检查是否应该停止
		select {
		case <-r.stopChan:
			r.setState(StateStopped)
			return errors.New("reconnector stopped")
		case <-ctx.Done():
			r.setState(StateDisconnected)
			return ctx.Err()
		default:
		}

		// 计算延迟
		delay := r.calculateDelay(attempt)

		if r.config.OnReconnect != nil {
			r.config.OnReconnect(attempt, delay)
		}

		r.logger.Info("reconnecting",
			zap.Int("attempt", attempt),
			zap.Duration("delay", delay),
		)

		// 等待重连
		select {
		case <-time.After(delay):
			r.incrementAttempt()
		case <-r.stopChan:
			r.setState(StateStopped)
			return errors.New("reconnector stopped")
		case <-ctx.Done():
			r.setState(StateDisconnected)
			return ctx.Err()
		}
	}
}

// HandleDisconnect 处理断开连接
func (r *Reconnector) HandleDisconnect(ctx context.Context, err error, reconnectFunc func() error) {
	r.mu.Lock()
	r.state = StateDisconnected
	r.mu.Unlock()

	if r.config.OnDisconnect != nil {
		r.config.OnDisconnect(err)
	}

	if !r.config.Enabled {
		return
	}

	go func() {
		if reconnectErr := r.Connect(ctx, func(ctx context.Context) error {
			return reconnectFunc()
		}); reconnectErr != nil {
			r.logger.Error("reconnection failed", zap.Error(reconnectErr))
		}
	}()
}

// Stop 停止重连
func (r *Reconnector) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()

	select {
	case <-r.stopChan:
		// 已经关闭
	default:
		close(r.stopChan)
	}
	r.state = StateStopped
}

// GetState 获取状态
func (r *Reconnector) GetState() ReconnectState {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.state
}

// GetAttempt 获取当前尝试次数
func (r *Reconnector) GetAttempt() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.attempt
}

// Reset 重置状态
func (r *Reconnector) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.attempt = 0
	r.state = StateDisconnected
}

func (r *Reconnector) getAttempt() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.attempt + 1
}

func (r *Reconnector) incrementAttempt() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.attempt++
}

func (r *Reconnector) resetAttempt() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.attempt = 0
}

func (r *Reconnector) setState(state ReconnectState) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.state = state
}

func (r *Reconnector) calculateDelay(attempt int) time.Duration {
	delay := r.config.InitialDelay
	for i := 1; i < attempt; i++ {
		delay = time.Duration(float64(delay) * r.config.Multiplier)
		if delay > r.config.MaxDelay {
			delay = r.config.MaxDelay
			break
		}
	}

	if r.config.Jitter {
		// 添加最多 20% 的随机抖动
		jitter := time.Duration(rand.Float64() * 0.2 * float64(delay))
		delay += jitter
	}

	return delay
}

// ConnectionState 连接状态管理器
type ConnectionState struct {
	connectedAt time.Time
	lastError   error
	state       ReconnectState
	mu          sync.RWMutex
}

// NewConnectionState 创建连接状态
func NewConnectionState() *ConnectionState {
	return &ConnectionState{
		state: StateDisconnected,
	}
}

// SetConnected 设置已连接
func (s *ConnectionState) SetConnected() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state = StateConnected
	s.connectedAt = time.Now()
	s.lastError = nil
}

// SetDisconnected 设置已断开
func (s *ConnectionState) SetDisconnected(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state = StateDisconnected
	s.lastError = err
}

// SetReconnecting 设置重连中
func (s *ConnectionState) SetReconnecting() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state = StateReconnecting
}

// GetState 获取状态
func (s *ConnectionState) GetState() ReconnectState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state
}

// GetConnectedAt 获取连接时间
func (s *ConnectionState) GetConnectedAt() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.connectedAt
}

// GetLastError 获取最后的错误
func (s *ConnectionState) GetLastError() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastError
}

// Uptime 获取连接时长
func (s *ConnectionState) Uptime() time.Duration {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.state != StateConnected {
		return 0
	}
	return time.Since(s.connectedAt)
}

// IsConnected 检查是否已连接
func (s *ConnectionState) IsConnected() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state == StateConnected
}