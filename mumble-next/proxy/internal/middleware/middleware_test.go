package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestChain(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	// 测试空中间件链
	h := Chain(handler)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}

func TestLogging(t *testing.T) {
	logger := zap.NewNop()
	middleware := Logging(logger)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	middleware(handler).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}

func TestRequestID(t *testing.T) {
	middleware := RequestID("X-Request-ID")

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := GetRequestID(r.Context())
		if requestID == "" {
			t.Error("expected request ID to be set")
		}
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	middleware(handler).ServeHTTP(rec, req)

	if rec.Header().Get("X-Request-ID") == "" {
		t.Error("expected X-Request-ID header to be set")
	}
}

func TestRequestID_Existing(t *testing.T) {
	middleware := RequestID("X-Request-ID")

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := GetRequestID(r.Context())
		if requestID != "existing-id" {
			t.Errorf("expected request ID 'existing-id', got %s", requestID)
		}
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-ID", "existing-id")
	rec := httptest.NewRecorder()

	middleware(handler).ServeHTTP(rec, req)

	if rec.Header().Get("X-Request-ID") != "existing-id" {
		t.Error("expected existing X-Request-ID to be preserved")
	}
}

func TestCORS(t *testing.T) {
	tests := []struct {
		name           string
		origins        []string
		requestOrigin  string
		expectHeader   string
		expectStatus   int
		method         string
	}{
		{
			name:         "wildcard origin",
			origins:      []string{"*"},
			requestOrigin: "http://example.com",
			expectHeader: "http://example.com",
			expectStatus: http.StatusOK,
			method:       http.MethodGet,
		},
		{
			name:         "specific origin allowed",
			origins:      []string{"http://example.com"},
			requestOrigin: "http://example.com",
			expectHeader: "http://example.com",
			expectStatus: http.StatusOK,
			method:       http.MethodGet,
		},
		{
			name:         "origin not allowed",
			origins:      []string{"http://allowed.com"},
			requestOrigin: "http://notallowed.com",
			expectHeader: "",
			expectStatus: http.StatusOK,
			method:       http.MethodGet,
		},
		{
			name:         "options preflight",
			origins:      []string{"*"},
			requestOrigin: "http://example.com",
			expectHeader: "http://example.com",
			expectStatus: http.StatusOK,
			method:       http.MethodOptions,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			middleware := CORS(tt.origins)

			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			})

			req := httptest.NewRequest(tt.method, "/", nil)
			if tt.requestOrigin != "" {
				req.Header.Set("Origin", tt.requestOrigin)
			}
			rec := httptest.NewRecorder()

			middleware(handler).ServeHTTP(rec, req)

			if rec.Code != tt.expectStatus {
				t.Errorf("expected status %d, got %d", tt.expectStatus, rec.Code)
			}

			if tt.expectHeader != "" {
				if rec.Header().Get("Access-Control-Allow-Origin") != tt.expectHeader {
					t.Errorf("expected Allow-Origin %s, got %s", tt.expectHeader, rec.Header().Get("Access-Control-Allow-Origin"))
				}
			}
		})
	}
}

func TestRecover(t *testing.T) {
	logger := zap.NewNop()
	middleware := Recover(logger)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("test panic")
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	middleware(handler).ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", rec.Code)
	}
}

func TestRecover_NoPanic(t *testing.T) {
	logger := zap.NewNop()
	middleware := Recover(logger)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	middleware(handler).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}

// MockRateLimiter 模拟速率限制器
type MockRateLimiter struct {
	allowed bool
}

func (m *MockRateLimiter) Allow(ip string) bool {
	return m.allowed
}

func TestRateLimit(t *testing.T) {
	limiter := &MockRateLimiter{allowed: true}
	middleware := RateLimit(limiter)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	middleware(handler).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}

func TestRateLimit_Denied(t *testing.T) {
	limiter := &MockRateLimiter{allowed: false}
	middleware := RateLimit(limiter)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	middleware(handler).ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("expected status 429, got %d", rec.Code)
	}
}

func TestTimeout(t *testing.T) {
	middleware := Timeout(5 * time.Second)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	middleware(handler).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}

func TestTimeout_Exceeded(t *testing.T) {
	middleware := Timeout(10 * time.Millisecond)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	middleware(handler).ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestTimeout {
		t.Errorf("expected status 408, got %d", rec.Code)
	}
}

// MockTokenManager 模拟 Token 管理器
type MockTokenManager struct {
	backend interface{}
	valid   bool
}

func (m *MockTokenManager) Validate(token string) (interface{}, bool) {
	return m.backend, m.valid
}

func TestAuth_Success(t *testing.T) {
	tm := &MockTokenManager{backend: "test-backend", valid: true}
	middleware := Auth(tm, "X-Token", "token")

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		backend := GetBackend(r.Context())
		if backend != "test-backend" {
			t.Errorf("expected backend 'test-backend', got %v", backend)
		}
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Token", "valid-token")
	rec := httptest.NewRecorder()

	middleware(handler).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}

func TestAuth_TokenFromQuery(t *testing.T) {
	tm := &MockTokenManager{backend: "test-backend", valid: true}
	middleware := Auth(tm, "X-Token", "token")

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		backend := GetBackend(r.Context())
		if backend != "test-backend" {
			t.Errorf("expected backend 'test-backend', got %v", backend)
		}
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/?token=valid-token", nil)
	rec := httptest.NewRecorder()

	middleware(handler).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}

func TestAuth_Unauthorized(t *testing.T) {
	tm := &MockTokenManager{backend: nil, valid: false}
	middleware := Auth(tm, "X-Token", "token")

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Token", "invalid-token")
	rec := httptest.NewRecorder()

	middleware(handler).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", rec.Code)
	}
}

func TestGetBackend_Nil(t *testing.T) {
	ctx := context.Background()
	backend := GetBackend(ctx)
	if backend != nil {
		t.Errorf("expected nil backend, got %v", backend)
	}
}

func TestChain_MultipleMiddleware(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Chain multiple middleware
	h := Chain(handler, RequestID("X-Request-ID"), Logging(zap.NewNop()))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}