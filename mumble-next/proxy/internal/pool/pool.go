package pool

import (
	"context"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

// Pool 连接池接口
type Pool interface {
	Get(ctx context.Context) (net.Conn, error)
	Put(conn net.Conn)
	Close()
	Stats() PoolStats
}

// PoolStats 连接池统计
type PoolStats struct {
	TotalConns     int64
	IdleConns      int64
	ActiveConns    int64
	WaitCount      int64
	WaitDuration   time.Duration
	CreateCount    int64
	CreateErrors   int64
	TimeoutErrors  int64
}

// ConnPool TCP 连接池
type ConnPool struct {
	host        string
	port        int
	maxIdle     int
	maxActive   int
	idleTimeout time.Duration
	connTimeout time.Duration

	mu        sync.Mutex
	idle      chan *poolConn
	active    int64
	waitCount int64
	waitTime  int64

	// 统计
	createCount   atomic.Int64
	createErrors  atomic.Int64
	timeoutErrors atomic.Int64

	logger   *zap.Logger
	stopCh   chan struct{}
	stopped  atomic.Bool
}

type poolConn struct {
	net.Conn
	createdAt time.Time
	lastUsed  time.Time
}

// NewConnPool 创建连接池
func NewConnPool(host string, port int, maxIdle, maxActive int, idleTimeout, connTimeout time.Duration, logger *zap.Logger) *ConnPool {
	p := &ConnPool{
		host:        host,
		port:        port,
		maxIdle:     maxIdle,
		maxActive:   maxActive,
		idleTimeout: idleTimeout,
		connTimeout: connTimeout,
		idle:        make(chan *poolConn, maxIdle),
		logger:      logger,
		stopCh:      make(chan struct{}),
	}

	// 启动空闲连接清理
	go p.cleanupLoop()

	return p
}

// Get 获取连接
func (p *ConnPool) Get(ctx context.Context) (net.Conn, error) {
	if p.stopped.Load() {
		return nil, fmt.Errorf("connection pool closed")
	}

	// 尝试从空闲队列获取
	select {
	case pc := <-p.idle:
		if p.isConnStale(pc) {
			pc.Conn.Close()
			atomic.AddInt64(&p.active, 1)
			return p.createNew()
		}
		atomic.AddInt64(&p.active, 1)
		pc.lastUsed = time.Now()
		return &pooledConn{Conn: pc.Conn, pool: p}, nil
	default:
	}

	// 检查是否达到最大活跃数
	if p.maxActive > 0 && atomic.LoadInt64(&p.active) >= int64(p.maxActive) {
		p.mu.Lock()
		p.waitCount++
		p.mu.Unlock()

		// 等待可用连接
		select {
		case pc := <-p.idle:
			if p.isConnStale(pc) {
				pc.Conn.Close()
				return p.createNew()
			}
			atomic.AddInt64(&p.active, 1)
			pc.lastUsed = time.Now()
			return &pooledConn{Conn: pc.Conn, pool: p}, nil
		case <-ctx.Done():
			p.timeoutErrors.Add(1)
			return nil, fmt.Errorf("connection pool timeout: %w", ctx.Err())
		case <-p.stopCh:
			return nil, fmt.Errorf("connection pool closed")
		}
	}

	return p.createNew()
}

func (p *ConnPool) createNew() (net.Conn, error) {
	p.createCount.Add(1)

	addr := net.JoinHostPort(p.host, fmt.Sprintf("%d", p.port))
	conn, err := net.DialTimeout("tcp", addr, p.connTimeout)
	if err != nil {
		p.createErrors.Add(1)
		return nil, fmt.Errorf("failed to connect to %s: %w", addr, err)
	}

	atomic.AddInt64(&p.active, 1)
	return &pooledConn{Conn: conn, pool: p}, nil
}

// Put 归还连接
func (p *ConnPool) Put(conn net.Conn) {
	if conn == nil {
		return
	}

	atomic.AddInt64(&p.active, -1)

	if p.stopped.Load() {
		conn.Close()
		return
	}

	pc := &poolConn{
		Conn:      conn,
		createdAt: time.Now(),
		lastUsed:  time.Now(),
	}

	select {
	case p.idle <- pc:
		// 成功放入空闲队列
	default:
		// 空闲队列已满，关闭连接
		conn.Close()
	}
}

// Close 关闭连接池
func (p *ConnPool) Close() {
	if p.stopped.Swap(true) {
		return // 已经关闭
	}

	close(p.stopCh)

	// 清空空闲连接
	for {
		select {
		case pc := <-p.idle:
			pc.Conn.Close()
		default:
			return
		}
	}
}

// Stats 获取统计信息
func (p *ConnPool) Stats() PoolStats {
	return PoolStats{
		TotalConns:   atomic.LoadInt64(&p.active) + int64(len(p.idle)),
		IdleConns:    int64(len(p.idle)),
		ActiveConns:  atomic.LoadInt64(&p.active),
		WaitCount:    p.waitCount,
		WaitDuration: time.Duration(atomic.LoadInt64(&p.waitTime)),
		CreateCount:  p.createCount.Load(),
		CreateErrors: p.createErrors.Load(),
		TimeoutErrors: p.timeoutErrors.Load(),
	}
}

func (p *ConnPool) isConnStale(pc *poolConn) bool {
	return time.Since(pc.lastUsed) > p.idleTimeout
}

func (p *ConnPool) cleanupLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			p.cleanupStale()
		case <-p.stopCh:
			return
		}
	}
}

func (p *ConnPool) cleanupStale() {
	for len(p.idle) > 0 {
		select {
		case pc := <-p.idle:
			if p.isConnStale(pc) {
				pc.Conn.Close()
			} else {
				// 放回去
				select {
				case p.idle <- pc:
				default:
					pc.Conn.Close()
				}
				return
			}
		default:
			return
		}
	}
}

// pooledConn 包装的连接
type pooledConn struct {
	net.Conn
	pool *ConnPool
}

func (c *pooledConn) Close() error {
	c.pool.Put(c.Conn)
	return nil
}

// HealthChecker 连接池健康检查器
type HealthChecker struct {
	pool   *ConnPool
	logger *zap.Logger
}

// NewHealthChecker 创建健康检查器
func NewHealthChecker(pool *ConnPool, logger *zap.Logger) *HealthChecker {
	return &HealthChecker{
		pool:   pool,
		logger: logger,
	}
}

// Check 执行健康检查
func (h *HealthChecker) Check() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := h.pool.Get(ctx)
	if err != nil {
		return err
	}

	conn.Close()
	return nil
}