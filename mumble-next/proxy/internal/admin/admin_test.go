package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.uber.org/zap"

	"github.com/mumble/mumble-next/proxy/internal/auth"
	"github.com/mumble/mumble-next/proxy/internal/config"
	"github.com/mumble/mumble-next/proxy/internal/metrics"
	"github.com/mumble/mumble-next/proxy/internal/proxy"
)

func TestNewHandler(t *testing.T) {
	cfg := config.DefaultConfig()
	logger := zap.NewNop()
	m := metrics.NewNoopMetrics()

	p := proxy.NewProxyWithMetrics(cfg, logger, m)
	tm := auth.NewTokenManager()
	tm.LoadFromConfig(cfg)

	handler := NewHandler(p, tm, cfg, logger)

	if handler == nil {
		t.Fatal("expected handler to be created")
	}
}

func TestHandler_ListBackends(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Backends = []config.BackendConfig{
		{Name: "backend1", Host: "server1.example.com", Port: 64738, Tokens: []string{"token1"}},
		{Name: "backend2", Host: "server2.example.com", Port: 64738, Tokens: []string{"token2"}},
	}

	logger := zap.NewNop()
	m := metrics.NewNoopMetrics()

	p := proxy.NewProxyWithMetrics(cfg, logger, m)
	tm := auth.NewTokenManager()
	tm.LoadFromConfig(cfg)

	handler := NewHandler(p, tm, cfg, logger)

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/admin/backends", nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	var backends []BackendInfo
	if err := json.Unmarshal(rec.Body.Bytes(), &backends); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	// 应该有 3 个后端 (default + 2 configured)
	if len(backends) != 3 {
		t.Errorf("expected 3 backends, got %d", len(backends))
	}
}

func TestHandler_CreateBackend(t *testing.T) {
	cfg := config.DefaultConfig()
	logger := zap.NewNop()
	m := metrics.NewNoopMetrics()

	p := proxy.NewProxyWithMetrics(cfg, logger, m)
	tm := auth.NewTokenManager()
	tm.LoadFromConfig(cfg)

	handler := NewHandler(p, tm, cfg, logger)

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	body := `{"name":"new-backend","host":"new.example.com","port":64738,"tokens":["new-token"]}`
	req := httptest.NewRequest(http.MethodPost, "/admin/backends", strings.NewReader(body))
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	// 验证后端已创建
	backend, ok := tm.GetBackend("new-backend")
	if !ok {
		t.Fatal("expected backend to be created")
	}

	if backend.Host != "new.example.com" {
		t.Errorf("expected host 'new.example.com', got %s", backend.Host)
	}
}

func TestHandler_DeleteBackend(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Backends = []config.BackendConfig{
		{Name: "backend1", Host: "server1.example.com", Port: 64738, Tokens: []string{"token1"}},
	}

	logger := zap.NewNop()
	m := metrics.NewNoopMetrics()

	p := proxy.NewProxyWithMetrics(cfg, logger, m)
	tm := auth.NewTokenManager()
	tm.LoadFromConfig(cfg)

	handler := NewHandler(p, tm, cfg, logger)

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodDelete, "/admin/backends/backend1", nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	// 验证后端已删除
	_, ok := tm.GetBackend("backend1")
	if ok {
		t.Error("expected backend to be deleted")
	}
}

func TestHandler_GetConnections(t *testing.T) {
	cfg := config.DefaultConfig()
	logger := zap.NewNop()
	m := metrics.NewNoopMetrics()

	p := proxy.NewProxyWithMetrics(cfg, logger, m)
	tm := auth.NewTokenManager()
	tm.LoadFromConfig(cfg)

	handler := NewHandler(p, tm, cfg, logger)

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/admin/connections", nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	var response map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if _, ok := response["active_connections"]; !ok {
		t.Error("expected active_connections in response")
	}
}

func TestHandler_GetConfig(t *testing.T) {
	cfg := config.DefaultConfig()
	logger := zap.NewNop()
	m := metrics.NewNoopMetrics()

	p := proxy.NewProxyWithMetrics(cfg, logger, m)
	tm := auth.NewTokenManager()
	tm.LoadFromConfig(cfg)

	handler := NewHandler(p, tm, cfg, logger)

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/admin/config", nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}

func TestHandler_Drain(t *testing.T) {
	cfg := config.DefaultConfig()
	logger := zap.NewNop()
	m := metrics.NewNoopMetrics()

	p := proxy.NewProxyWithMetrics(cfg, logger, m)
	tm := auth.NewTokenManager()
	tm.LoadFromConfig(cfg)

	handler := NewHandler(p, tm, cfg, logger)

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodPost, "/admin/drain", nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}

func TestHandler_GetBackend(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Backends = []config.BackendConfig{
		{Name: "test-backend", Host: "test.example.com", Port: 64738, Tokens: []string{"token1"}},
	}

	logger := zap.NewNop()
	m := metrics.NewNoopMetrics()

	p := proxy.NewProxyWithMetrics(cfg, logger, m)
	tm := auth.NewTokenManager()
	tm.LoadFromConfig(cfg)

	handler := NewHandler(p, tm, cfg, logger)

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/admin/backends/test-backend", nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}

func TestHandler_GetBackend_NotFound(t *testing.T) {
	cfg := config.DefaultConfig()
	logger := zap.NewNop()
	m := metrics.NewNoopMetrics()

	p := proxy.NewProxyWithMetrics(cfg, logger, m)
	tm := auth.NewTokenManager()
	tm.LoadFromConfig(cfg)

	handler := NewHandler(p, tm, cfg, logger)

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/admin/backends/nonexistent", nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", rec.Code)
	}
}

func TestHandler_CreateBackend_InvalidJSON(t *testing.T) {
	cfg := config.DefaultConfig()
	logger := zap.NewNop()
	m := metrics.NewNoopMetrics()

	p := proxy.NewProxyWithMetrics(cfg, logger, m)
	tm := auth.NewTokenManager()
	tm.LoadFromConfig(cfg)

	handler := NewHandler(p, tm, cfg, logger)

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodPost, "/admin/backends", strings.NewReader("invalid json"))
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rec.Code)
	}
}

func TestHandler_CreateBackend_MissingFields(t *testing.T) {
	cfg := config.DefaultConfig()
	logger := zap.NewNop()
	m := metrics.NewNoopMetrics()

	p := proxy.NewProxyWithMetrics(cfg, logger, m)
	tm := auth.NewTokenManager()
	tm.LoadFromConfig(cfg)

	handler := NewHandler(p, tm, cfg, logger)

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	body := `{"name":""}` // Missing required fields
	req := httptest.NewRequest(http.MethodPost, "/admin/backends", strings.NewReader(body))
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rec.Code)
	}
}

func TestHandler_HandleBackends_MethodNotAllowed(t *testing.T) {
	cfg := config.DefaultConfig()
	logger := zap.NewNop()
	m := metrics.NewNoopMetrics()

	p := proxy.NewProxyWithMetrics(cfg, logger, m)
	tm := auth.NewTokenManager()
	tm.LoadFromConfig(cfg)

	handler := NewHandler(p, tm, cfg, logger)

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodPut, "/admin/backends", nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", rec.Code)
	}
}

func TestHandler_HandleBackend_MethodNotAllowed(t *testing.T) {
	cfg := config.DefaultConfig()
	logger := zap.NewNop()
	m := metrics.NewNoopMetrics()

	p := proxy.NewProxyWithMetrics(cfg, logger, m)
	tm := auth.NewTokenManager()
	tm.LoadFromConfig(cfg)

	handler := NewHandler(p, tm, cfg, logger)

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodPost, "/admin/backends/test", nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", rec.Code)
	}
}

func TestHandler_HandleConnections_MethodNotAllowed(t *testing.T) {
	cfg := config.DefaultConfig()
	logger := zap.NewNop()
	m := metrics.NewNoopMetrics()

	p := proxy.NewProxyWithMetrics(cfg, logger, m)
	tm := auth.NewTokenManager()
	tm.LoadFromConfig(cfg)

	handler := NewHandler(p, tm, cfg, logger)

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodPost, "/admin/connections", nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", rec.Code)
	}
}

func TestHandler_HandleConfig_MethodNotAllowed(t *testing.T) {
	cfg := config.DefaultConfig()
	logger := zap.NewNop()
	m := metrics.NewNoopMetrics()

	p := proxy.NewProxyWithMetrics(cfg, logger, m)
	tm := auth.NewTokenManager()
	tm.LoadFromConfig(cfg)

	handler := NewHandler(p, tm, cfg, logger)

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodDelete, "/admin/config", nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", rec.Code)
	}
}

func TestHandler_HandleConfig_UpdateNotImplemented(t *testing.T) {
	cfg := config.DefaultConfig()
	logger := zap.NewNop()
	m := metrics.NewNoopMetrics()

	p := proxy.NewProxyWithMetrics(cfg, logger, m)
	tm := auth.NewTokenManager()
	tm.LoadFromConfig(cfg)

	handler := NewHandler(p, tm, cfg, logger)

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodPut, "/admin/config", strings.NewReader("{}"))
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotImplemented {
		t.Errorf("expected status 501, got %d", rec.Code)
	}
}

func TestHandler_HandleShutdown(t *testing.T) {
	cfg := config.DefaultConfig()
	logger := zap.NewNop()
	m := metrics.NewNoopMetrics()

	p := proxy.NewProxyWithMetrics(cfg, logger, m)
	tm := auth.NewTokenManager()
	tm.LoadFromConfig(cfg)

	handler := NewHandler(p, tm, cfg, logger)

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodPost, "/admin/shutdown", nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}

func TestHandler_HandleShutdown_MethodNotAllowed(t *testing.T) {
	cfg := config.DefaultConfig()
	logger := zap.NewNop()
	m := metrics.NewNoopMetrics()

	p := proxy.NewProxyWithMetrics(cfg, logger, m)
	tm := auth.NewTokenManager()
	tm.LoadFromConfig(cfg)

	handler := NewHandler(p, tm, cfg, logger)

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/admin/shutdown", nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", rec.Code)
	}
}

func TestHandler_HandleDrain_MethodNotAllowed(t *testing.T) {
	cfg := config.DefaultConfig()
	logger := zap.NewNop()
	m := metrics.NewNoopMetrics()

	p := proxy.NewProxyWithMetrics(cfg, logger, m)
	tm := auth.NewTokenManager()
	tm.LoadFromConfig(cfg)

	handler := NewHandler(p, tm, cfg, logger)

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/admin/drain", nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", rec.Code)
	}
}