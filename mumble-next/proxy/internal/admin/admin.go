package admin

import (
	"encoding/json"
	"net/http"
	"time"

	"go.uber.org/zap"

	"github.com/mumble/mumble-next/proxy/internal/auth"
	"github.com/mumble/mumble-next/proxy/internal/config"
	"github.com/mumble/mumble-next/proxy/internal/proxy"
)

// Handler 管理 API 处理器
type Handler struct {
	proxy      *proxy.ProxyImpl
	tokenMgr   *auth.TokenManager
	config     *config.Config
	logger     *zap.Logger
}

// NewHandler 创建管理 API 处理器
func NewHandler(p *proxy.ProxyImpl, tm *auth.TokenManager, cfg *config.Config, logger *zap.Logger) *Handler {
	return &Handler{
		proxy:    p,
		tokenMgr: tm,
		config:   cfg,
		logger:   logger,
	}
}

// RegisterRoutes 注册路由
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	// 后端管理
	mux.HandleFunc("/admin/backends", h.handleBackends)
	mux.HandleFunc("/admin/backends/", h.handleBackend)

	// 连接管理
	mux.HandleFunc("/admin/connections", h.handleConnections)

	// 配置管理
	mux.HandleFunc("/admin/config", h.handleConfig)

	// 服务器控制
	mux.HandleFunc("/admin/shutdown", h.handleShutdown)
	mux.HandleFunc("/admin/drain", h.handleDrain)
}

// BackendInfo 后端信息
type BackendInfo struct {
	Name          string `json:"name"`
	Host          string `json:"host"`
	Port          int    `json:"port"`
	TokenCount    int    `json:"token_count"`
	ActiveConns   int64  `json:"active_connections"`
}

// ConnectionInfo 连接信息
type ConnectionInfo struct {
	ID          string    `json:"id"`
	RemoteAddr  string    `json:"remote_addr"`
	Backend     string    `json:"backend"`
	ConnectedAt time.Time `json:"connected_at"`
	Duration    string    `json:"duration"`
}

// handleBackends 处理后端列表
func (h *Handler) handleBackends(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.listBackends(w, r)
	case http.MethodPost:
		h.createBackend(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *Handler) listBackends(w http.ResponseWriter, r *http.Request) {
	backends := h.tokenMgr.ListBackends()

	infos := make([]BackendInfo, len(backends))
	for i, b := range backends {
		infos[i] = BackendInfo{
			Name:       b.Name,
			Host:       b.Host,
			Port:       b.Port,
			TokenCount: countTokensForBackend(h.config, b.Name),
		}
	}

	h.respondJSON(w, infos)
}

func (h *Handler) createBackend(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name   string   `json:"name"`
		Host   string   `json:"host"`
		Port   int      `json:"port"`
		Tokens []string `json:"tokens"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Name == "" || req.Host == "" || req.Port == 0 {
		http.Error(w, "Missing required fields", http.StatusBadRequest)
		return
	}

	h.tokenMgr.AddBackend(req.Name, req.Host, req.Port, req.Tokens)

	h.logger.Info("backend created via admin API",
		zap.String("name", req.Name),
		zap.String("host", req.Host),
		zap.Int("port", req.Port),
	)

	h.respondJSON(w, map[string]string{
		"status": "created",
		"name":   req.Name,
	})
}

// handleBackend 处理单个后端
func (h *Handler) handleBackend(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Path[len("/admin/backends/"):]

	switch r.Method {
	case http.MethodGet:
		h.getBackend(w, r, name)
	case http.MethodDelete:
		h.deleteBackend(w, r, name)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *Handler) getBackend(w http.ResponseWriter, r *http.Request, name string) {
	backend, ok := h.tokenMgr.GetBackend(name)
	if !ok {
		http.Error(w, "Backend not found", http.StatusNotFound)
		return
	}

	info := BackendInfo{
		Name:       backend.Name,
		Host:       backend.Host,
		Port:       backend.Port,
		TokenCount: countTokensForBackend(h.config, name),
	}

	h.respondJSON(w, info)
}

func (h *Handler) deleteBackend(w http.ResponseWriter, r *http.Request, name string) {
	h.tokenMgr.RemoveBackend(name)

	h.logger.Info("backend deleted via admin API",
		zap.String("name", name),
	)

	h.respondJSON(w, map[string]string{
		"status": "deleted",
		"name":   name,
	})
}

// handleConnections 处理连接列表
func (h *Handler) handleConnections(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	stats := h.proxy.Stats()

	response := map[string]interface{}{
		"active_connections": stats.ActiveConnections,
		"total_connections":  stats.TotalConnections,
		"bytes_sent":         stats.BytesSent,
		"bytes_received":     stats.BytesReceived,
		"uptime":             stats.Uptime.String(),
	}

	h.respondJSON(w, response)
}

// handleConfig 处理配置
func (h *Handler) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.getConfig(w, r)
	case http.MethodPut:
		h.updateConfig(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *Handler) getConfig(w http.ResponseWriter, r *http.Request) {
	h.respondJSON(w, h.config)
}

func (h *Handler) updateConfig(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "Not implemented", http.StatusNotImplemented)
}

// handleShutdown 处理关闭请求
func (h *Handler) handleShutdown(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	h.logger.Info("shutdown requested via admin API")

	h.respondJSON(w, map[string]string{
		"status": "shutting_down",
	})
}

// handleDrain 处理排空请求
func (h *Handler) handleDrain(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	stats := h.proxy.Stats()

	h.logger.Info("drain requested via admin API",
		zap.Int64("active_connections", stats.ActiveConnections),
	)

	h.respondJSON(w, map[string]interface{}{
		"status":             "draining",
		"active_connections": stats.ActiveConnections,
	})
}

// respondJSON 返回 JSON 响应
func (h *Handler) respondJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

// 辅助函数
func countTokensForBackend(cfg *config.Config, name string) int {
	for _, b := range cfg.Backends {
		if b.Name == name {
			return len(b.Tokens)
		}
	}
	return 0
}