package proxy

import (
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	"github.com/mumble/mumble-next/proxy/internal/auth"
)

// HandleWebSocket 处理 WebSocket 连接
func (p *ProxyImpl) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	// 获取客户端 IP
	ip := getClientIP(r)

	// 检查连接限制
	if !p.rateLimiter.Allow(ip) {
		p.logger.Warn("connection limit exceeded",
			zap.String("ip", ip),
		)
		http.Error(w, "connection limit exceeded", http.StatusTooManyRequests)
		return
	}

	// 获取 Token 并验证
	var backend *auth.Backend
	if p.config.Auth.Enabled {
		token := getToken(r, p.config.Auth.HeaderName, p.config.Auth.QueryParam)
		var ok bool
		backend, ok = p.tokenMgr.Validate(token)
		if !ok {
			p.logger.Warn("invalid token",
				zap.String("ip", ip),
				zap.String("token", maskToken(token)),
			)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			p.rateLimiter.Release(ip)
			return
		}
	} else {
		backend = p.tokenMgr.GetDefaultBackend()
	}

	// 升级 WebSocket
	ws, err := p.upgrader.Upgrade(w, r, nil)
	if err != nil {
		p.logger.Error("failed to upgrade websocket",
			zap.Error(err),
			zap.String("ip", ip),
		)
		p.rateLimiter.Release(ip)
		return
	}

	// 创建客户端连接
	clientID := generateClientID()
	client := &ClientConn{
		ID:         clientID,
		WsConn:     ws,
		Backend:    backend,
		RemoteAddr: ip,
		StartTime:  time.Now(),
		Done:       make(chan struct{}),
	}

	// 连接到 Murmur
	murmurAddr := fmt.Sprintf("%s:%d", backend.Host, backend.Port)
	tcp, err := connectMurmur(murmurAddr, p.config.Murmur.Timeout)
	if err != nil {
		p.logger.Error("failed to connect to murmur",
			zap.Error(err),
			zap.String("backend", backend.Name),
			zap.String("murmur_addr", murmurAddr),
		)
		ws.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(1011, "unable to connect to server"))
		ws.Close()
		p.rateLimiter.Release(ip)
		p.metrics.RecordError("murmur_connection", backend.Name)
		return
	}
	client.TcpConn = tcp

	// 记录连接
	p.mu.Lock()
	p.clients[clientID] = client
	p.mu.Unlock()

	p.activeConns.Add(1)
	p.totalConns.Add(1)
	p.metrics.IncrementConnections(backend.Name)

	p.logger.Info("client connected",
		zap.String("client_id", clientID),
		zap.String("ip", ip),
		zap.String("backend", backend.Name),
		zap.String("murmur_addr", murmurAddr),
	)

	// 清理函数
	cleanup := func() {
		p.mu.Lock()
		delete(p.clients, clientID)
		p.mu.Unlock()

		p.activeConns.Add(-1)
		p.rateLimiter.Release(ip)
		p.metrics.DecrementConnections(backend.Name)
		p.metrics.RecordConnectionDuration(time.Since(client.StartTime).Seconds())

		ws.Close()
		tcp.Close()

		p.logger.Info("client disconnected",
			zap.String("client_id", clientID),
			zap.String("ip", ip),
			zap.String("backend", backend.Name),
		)
	}

	// 双向转发
	forwarder := NewForwarder(ws, tcp, 32*1024)

	go func() {
		defer func() { close(client.Done) }()
		forwarder.ForwardWebSocketToTCP(func(err error) {
			p.logger.Debug("ws->tcp forward ended",
				zap.String("client_id", clientID),
				zap.Error(err),
			)
		})
	}()

	go func() {
		defer cleanup()
		forwarder.ForwardTCPToWebSocket(func(err error) {
			p.logger.Debug("tcp->ws forward ended",
				zap.String("client_id", clientID),
				zap.Error(err),
			)
		})
	}()

	// 等待连接关闭
	select {
	case <-client.Done:
	case <-p.stopCh:
		ws.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(1001, "server shutting down"))
	}
}

// WebSocketHandler WebSocket 处理器结构
type WebSocketHandler struct {
	proxy   *ProxyImpl
	logger  *zap.Logger
}

// NewWebSocketHandler 创建 WebSocket 处理器
func NewWebSocketHandler(proxy *ProxyImpl, logger *zap.Logger) *WebSocketHandler {
	return &WebSocketHandler{
		proxy:  proxy,
		logger: logger,
	}
}

// ServeHTTP 实现 http.Handler 接口
func (h *WebSocketHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.proxy.HandleWebSocket(w, r)
}

// 辅助函数

// getClientIP 获取客户端 IP
func getClientIP(r *http.Request) string {
	// 检查代理头
	if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
		return ip
	}
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}
	// 使用 RemoteAddr
	return r.RemoteAddr
}

// getToken 从请求中获取 Token
func getToken(r *http.Request, headerName, queryParam string) string {
	// 首先检查 Header
	if token := r.Header.Get(headerName); token != "" {
		return token
	}
	// 然后检查 URL 参数
	if token := r.URL.Query().Get(queryParam); token != "" {
		return token
	}
	return ""
}

// maskToken 遮蔽 Token
func maskToken(token string) string {
	if len(token) <= 4 {
		return "****"
	}
	return token[:4] + "****"
}

// generateClientID 生成客户端 ID
func generateClientID() string {
	return fmt.Sprintf("%d-%d", time.Now().UnixNano(), rand.Int63())
}

// connectMurmur 连接到 Murmur 服务器
func connectMurmur(addr string, timeout time.Duration) (net.Conn, error) {
	return net.DialTimeout("tcp", addr, timeout)
}