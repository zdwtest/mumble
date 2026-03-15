package proxy

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	"github.com/mumble/mumble-next/proxy/internal/auth"
	"github.com/mumble/mumble-next/proxy/internal/config"
	"github.com/mumble/mumble-next/proxy/internal/health"
	"github.com/mumble/mumble-next/proxy/internal/metrics"
)

// Stats 代理统计信息
type Stats struct {
	ActiveConnections int64         `json:"active_connections"`
	TotalConnections  int64         `json:"total_connections"`
	BytesSent         int64         `json:"bytes_sent"`
	BytesReceived     int64         `json:"bytes_received"`
	Uptime            time.Duration `json:"uptime"`
}

// Proxy 代理接口
type Proxy interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	Stats() Stats
}

// ProxyImpl 代理实现
type ProxyImpl struct {
	config    *config.Config
	tokenMgr  *auth.TokenManager
	metrics   *metrics.Metrics
	logger    *zap.Logger

	clients   map[string]*ClientConn
	mu        sync.RWMutex

	startTime time.Time
	httpSrv   *http.Server

	// 统计
	activeConns atomic.Int64
	totalConns  atomic.Int64
	bytesSent   atomic.Int64
	bytesRecv   atomic.Int64

	// WebSocket 升级器
	upgrader websocket.Upgrader

	// 限流器
	rateLimiter *RateLimiter

	// 关闭信号
	stopCh chan struct{}
}

// ClientConn 客户端连接
type ClientConn struct {
	ID         string
	WsConn     *websocket.Conn
	TcpConn    net.Conn
	Backend    *auth.Backend
	RemoteAddr string
	StartTime  time.Time
	Done       chan struct{}
}

// NewProxy 创建代理
func NewProxy(cfg *config.Config, logger *zap.Logger) *ProxyImpl {
	tokenMgr := auth.NewTokenManager()
	tokenMgr.LoadFromConfig(cfg)

	return &ProxyImpl{
		config:     cfg,
		tokenMgr:   tokenMgr,
		metrics:    metrics.NewMetrics(),
		logger:     logger,
		clients:    make(map[string]*ClientConn),
		startTime:  time.Now(),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true // 允许所有来源
			},
			ReadBufferSize:  32 * 1024,
			WriteBufferSize: 32 * 1024,
		},
		rateLimiter: NewRateLimiter(cfg.Limits.MaxConnectionsPerIP),
		stopCh:      make(chan struct{}),
	}
}

// NewProxyWithMetrics 创建带自定义指标的代理 (用于测试)
func NewProxyWithMetrics(cfg *config.Config, logger *zap.Logger, m *metrics.Metrics) *ProxyImpl {
	tokenMgr := auth.NewTokenManager()
	tokenMgr.LoadFromConfig(cfg)

	return &ProxyImpl{
		config:     cfg,
		tokenMgr:   tokenMgr,
		metrics:    m,
		logger:     logger,
		clients:    make(map[string]*ClientConn),
		startTime:  time.Now(),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true // 允许所有来源
			},
			ReadBufferSize:  32 * 1024,
			WriteBufferSize: 32 * 1024,
		},
		rateLimiter: NewRateLimiter(cfg.Limits.MaxConnectionsPerIP),
		stopCh:      make(chan struct{}),
	}
}

// Start 启动代理
func (p *ProxyImpl) Start(ctx context.Context) error {
	mux := http.NewServeMux()

	// WebSocket 端点
	mux.HandleFunc("/mumble", p.HandleWebSocket)

	// 静态文件
	mux.Handle("/", http.FileServer(http.Dir("./web")))

	// 统计端点
	mux.HandleFunc("/stats", p.HandleStats)

	addr := fmt.Sprintf("%s:%d", p.config.Server.Host, p.config.Server.Port)
	p.httpSrv = &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  p.config.Server.ReadTimeout,
		WriteTimeout: p.config.Server.WriteTimeout,
	}

	p.logger.Info("starting proxy server",
		zap.String("addr", addr),
		zap.String("murmur", fmt.Sprintf("%s:%d", p.config.Murmur.Host, p.config.Murmur.Port)),
	)

	// 启动 HTTP 服务器
	errCh := make(chan error, 1)
	go func() {
		if p.config.Server.TLS.Enabled {
			errCh <- p.httpSrv.ListenAndServeTLS(
				p.config.Server.TLS.CertFile,
				p.config.Server.TLS.KeyFile,
			)
		} else {
			errCh <- p.httpSrv.ListenAndServe()
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		return p.Stop(ctx)
	}
}

// Stop 停止代理
func (p *ProxyImpl) Stop(ctx context.Context) error {
	p.logger.Info("stopping proxy server")

	close(p.stopCh)

	// 关闭所有客户端连接
	p.mu.Lock()
	for _, client := range p.clients {
		client.WsConn.Close()
		if client.TcpConn != nil {
			client.TcpConn.Close()
		}
	}
	p.clients = make(map[string]*ClientConn)
	p.mu.Unlock()

	// 关闭 HTTP 服务器
	if p.httpSrv != nil {
		return p.httpSrv.Shutdown(ctx)
	}

	return nil
}

// Stats 获取统计信息
func (p *ProxyImpl) Stats() Stats {
	return Stats{
		ActiveConnections: p.activeConns.Load(),
		TotalConnections:  p.totalConns.Load(),
		BytesSent:         p.bytesSent.Load(),
		BytesReceived:     p.bytesRecv.Load(),
		Uptime:            time.Since(p.startTime),
	}
}

// HandleStats 处理统计请求
func (p *ProxyImpl) HandleStats(w http.ResponseWriter, r *http.Request) {
	stats := p.Stats()
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"status":"ok","active_connections":%d,"total_connections":%d,"bytes_sent":%d,"bytes_received":%d,"uptime":"%s"}`,
		stats.ActiveConnections, stats.TotalConnections, stats.BytesSent, stats.BytesReceived, stats.Uptime)
}

// GetMetrics 获取指标
func (p *ProxyImpl) GetMetrics() *metrics.Metrics {
	return p.metrics
}

// GetTokenManager 获取 Token 管理器
func (p *ProxyImpl) GetTokenManager() *auth.TokenManager {
	return p.tokenMgr
}

// RegisterHealthHandlers 注册健康检查处理器
func (p *ProxyImpl) RegisterHealthHandlers(mux *http.ServeMux) {
	checker := health.NewChecker(p.config.Murmur.Host, p.config.Murmur.Port)
	mux.HandleFunc("/health", checker.Handler())
	mux.HandleFunc("/healthz", checker.LivenessHandler())
	mux.HandleFunc("/readyz", checker.ReadinessHandler())
}