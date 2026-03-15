package proxy

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"go.uber.org/zap"
)

// RetryConfig 重试配置
type RetryConfig struct {
	// MaxAttempts 最大尝试次数
	MaxAttempts int
	// InitialDelay 初始延迟
	InitialDelay time.Duration
	// MaxDelay 最大延迟
	MaxDelay time.Duration
	// Multiplier 延迟乘数
	Multiplier float64
}

// DefaultRetryConfig 默认重试配置
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts:  3,
		InitialDelay: 100 * time.Millisecond,
		MaxDelay:     5 * time.Second,
		Multiplier:   2.0,
	}
}

// RetryableDialer 可重试的连接器
type RetryableDialer struct {
	config RetryConfig
	logger *zap.Logger
}

// NewRetryableDialer 创建可重试的连接器
func NewRetryableDialer(config RetryConfig, logger *zap.Logger) *RetryableDialer {
	return &RetryableDialer{
		config: config,
		logger: logger,
	}
}

// DialWithRetry 带重试的连接
func (d *RetryableDialer) DialWithRetry(ctx context.Context, network, addr string, timeout time.Duration) (net.Conn, error) {
	var lastErr error
	delay := d.config.InitialDelay

	// Handle nil context
	var doneCh <-chan struct{}
	if ctx != nil {
		doneCh = ctx.Done()
	}

	for attempt := 1; attempt <= d.config.MaxAttempts; attempt++ {
		select {
		case <-doneCh:
			if ctx != nil {
				return nil, ctx.Err()
			}
			return nil, context.Canceled
		default:
		}

		conn, err := net.DialTimeout(network, addr, timeout)
		if err == nil {
			if attempt > 1 {
				d.logger.Info("connection succeeded after retry",
					zap.Int("attempt", attempt),
					zap.String("addr", addr),
				)
			}
			return conn, nil
		}

		lastErr = err
		d.logger.Debug("connection attempt failed",
			zap.Int("attempt", attempt),
			zap.String("addr", addr),
			zap.Error(err),
		)

		// 检查是否是可重试的错误
		if !d.isRetryableError(err) {
			d.logger.Debug("non-retryable error",
				zap.Error(err),
			)
			break
		}

		// 最后一次尝试不等待
		if attempt < d.config.MaxAttempts {
			select {
			case <-doneCh:
				if ctx != nil {
					return nil, ctx.Err()
				}
				return nil, context.Canceled
			case <-time.After(delay):
			}

			// 计算下一次延迟
			delay = time.Duration(float64(delay) * d.config.Multiplier)
			if delay > d.config.MaxDelay {
				delay = d.config.MaxDelay
			}
		}
	}

	return nil, fmt.Errorf("failed after %d attempts: %w", d.config.MaxAttempts, lastErr)
}

// isRetryableError 检查是否是可重试的错误
func (d *RetryableDialer) isRetryableError(err error) bool {
	// 超时错误可以重试
	if netErr, ok := err.(net.Error); ok {
		if netErr.Timeout() {
			return true
		}
	}

	// 连接拒绝通常意味着服务不可用，可以重试
	if opErr, ok := err.(*net.OpError); ok {
		if opErr.Op == "dial" {
			return true
		}
	}

	return false
}

// ExponentialBackoff 指数退避
type ExponentialBackoff struct {
	initial    time.Duration
	max        time.Duration
	multiplier float64
	current    time.Duration
}

// NewExponentialBackoff 创建指数退避
func NewExponentialBackoff(initial, max time.Duration, multiplier float64) *ExponentialBackoff {
	return &ExponentialBackoff{
		initial:    initial,
		max:        max,
		multiplier: multiplier,
		current:    initial,
	}
}

// Next 获取下一次等待时间
func (b *ExponentialBackoff) Next() time.Duration {
	defer func() {
		b.current = time.Duration(float64(b.current) * b.multiplier)
		if b.current > b.max {
			b.current = b.max
		}
	}()
	return b.current
}

// Reset 重置退避
func (b *ExponentialBackoff) Reset() {
	b.current = b.initial
}

// CircuitBreaker 熔断器
type CircuitBreaker struct {
	maxFailures  int
	timeout      time.Duration
	failures     int
	lastFailTime time.Time
	state        CircuitState
	mu           sync.RWMutex
}

// CircuitState 熔断器状态
type CircuitState int

const (
	// StateClosed 关闭状态（正常）
	StateClosed CircuitState = iota
	// StateOpen 打开状态（熔断）
	StateOpen
	// StateHalfOpen 半开状态（尝试恢复）
	StateHalfOpen
)

// NewCircuitBreaker 创建熔断器
func NewCircuitBreaker(maxFailures int, timeout time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		maxFailures: maxFailures,
		timeout:     timeout,
		state:       StateClosed,
	}
}

// Allow 检查是否允许请求
func (cb *CircuitBreaker) Allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case StateClosed:
		return true
	case StateOpen:
		// 检查是否超时
		if time.Since(cb.lastFailTime) > cb.timeout {
			cb.state = StateHalfOpen
			return true
		}
		return false
	case StateHalfOpen:
		return true
	default:
		return false
	}
}

// RecordSuccess 记录成功
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failures = 0
	cb.state = StateClosed
}

// RecordFailure 记录失败
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failures++
	cb.lastFailTime = time.Now()

	if cb.failures >= cb.maxFailures {
		cb.state = StateOpen
	}
}

// State 获取当前状态
func (cb *CircuitBreaker) State() CircuitState {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}