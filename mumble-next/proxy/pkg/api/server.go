package api

import (
	"context"
	"fmt"
	"net/http"

	"go.uber.org/zap"

	"github.com/mumble/mumble-next/proxy/internal/config"
	"github.com/mumble/mumble-next/proxy/internal/proxy"
)

// Server HTTP 服务器
type Server struct {
	config     *config.Config
	proxy      *proxy.ProxyImpl
	logger     *zap.Logger
	httpServer *http.Server
}

// NewServer 创建 HTTP 服务器
func NewServer(cfg *config.Config, p *proxy.ProxyImpl, logger *zap.Logger) *Server {
	return &Server{
		config: cfg,
		proxy:  p,
		logger: logger,
	}
}

// Start 启动服务器
func (s *Server) Start(ctx context.Context) error {
	mux := http.NewServeMux()

	// 注册路由
	s.registerRoutes(mux)

	addr := fmt.Sprintf("%s:%d", s.config.Server.Host, s.config.Server.Port)
	s.httpServer = &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  s.config.Server.ReadTimeout,
		WriteTimeout: s.config.Server.WriteTimeout,
	}

	s.logger.Info("starting HTTP server",
		zap.String("addr", addr),
	)

	// 启动服务器
	errCh := make(chan error, 1)
	go func() {
		if s.config.Server.TLS.Enabled {
			errCh <- s.httpServer.ListenAndServeTLS(
				s.config.Server.TLS.CertFile,
				s.config.Server.TLS.KeyFile,
			)
		} else {
			errCh <- s.httpServer.ListenAndServe()
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		return s.Shutdown(context.Background())
	}
}

// Shutdown 关闭服务器
func (s *Server) Shutdown(ctx context.Context) error {
	if s.httpServer != nil {
		s.logger.Info("shutting down HTTP server")
		return s.httpServer.Shutdown(ctx)
	}
	return nil
}

// registerRoutes 注册路由
func (s *Server) registerRoutes(mux *http.ServeMux) {
	// WebSocket 端点
	mux.HandleFunc("/mumble", s.proxy.HandleWebSocket)

	// 静态文件
	mux.Handle("/", http.FileServer(http.Dir("./web")))

	// 统计端点
	mux.HandleFunc("/stats", s.proxy.HandleStats)

	// 健康检查
	s.proxy.RegisterHealthHandlers(mux)
}

// GetAddr 获取服务器地址
func (s *Server) GetAddr() string {
	if s.httpServer != nil {
		return s.httpServer.Addr
	}
	return fmt.Sprintf("%s:%d", s.config.Server.Host, s.config.Server.Port)
}