package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestNewProfilingHandler(t *testing.T) {
	tests := []struct {
		name   string
		config ProfilingHandlerConfig
	}{
		{
			name:   "default config",
			config: ProfilingHandlerConfig{},
		},
		{
			name: "with logger",
			config: ProfilingHandlerConfig{
				Logger: zap.NewNop(),
			},
		},
		{
			name: "with custom durations",
			config: ProfilingHandlerConfig{
				Logger:                zap.NewNop(),
				DefaultProfileDuration: 60 * time.Second,
				DefaultTraceDuration:   10 * time.Second,
				MaxProfileDuration:     10 * time.Minute,
				MaxTraceDuration:       2 * time.Minute,
			},
		},
		{
			name: "with rate limits",
			config: ProfilingHandlerConfig{
				Logger:           zap.NewNop(),
				ProfileRateLimit: 1 * time.Minute,
				TraceRateLimit:   2 * time.Minute,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewProfilingHandler(tt.config)
			if h == nil {
				t.Fatal("expected handler to be created")
			}
			if h.IsEnabled() {
				t.Error("expected handler to be disabled by default")
			}
		})
	}
}

func TestProfilingHandler_EnableDisable(t *testing.T) {
	h := NewProfilingHandler(ProfilingHandlerConfig{
		Logger: zap.NewNop(),
	})

	if h.IsEnabled() {
		t.Error("expected handler to be disabled initially")
	}

	h.Enable()
	if !h.IsEnabled() {
		t.Error("expected handler to be enabled after Enable()")
	}

	h.Disable()
	if h.IsEnabled() {
		t.Error("expected handler to be disabled after Disable()")
	}
}

func TestProfilingHandler_SetAuthTokens(t *testing.T) {
	h := NewProfilingHandler(ProfilingHandlerConfig{
		Logger: zap.NewNop(),
	})

	tokens := []string{"token1", "token2", "token3"}
	h.SetAuthTokens(tokens)

	h.mu.RLock()
	if len(h.authTokens) != 3 {
		t.Errorf("expected 3 tokens, got %d", len(h.authTokens))
	}
	for _, token := range tokens {
		if !h.authTokens[token] {
			t.Errorf("expected token %s to be set", token)
		}
	}
	h.mu.RUnlock()

	// Test with empty tokens
	h.SetAuthTokens([]string{})
	h.mu.RLock()
	if len(h.authTokens) != 0 {
		t.Errorf("expected 0 tokens, got %d", len(h.authTokens))
	}
	h.mu.RUnlock()

	// Test with empty strings
	h.SetAuthTokens([]string{"", "valid", ""})
	h.mu.RLock()
	if len(h.authTokens) != 1 {
		t.Errorf("expected 1 token (empty strings ignored), got %d", len(h.authTokens))
	}
	h.mu.RUnlock()
}

func TestProfilingHandler_AddRemoveAuthToken(t *testing.T) {
	h := NewProfilingHandler(ProfilingHandlerConfig{
		Logger: zap.NewNop(),
	})

	h.AddAuthToken("token1")
	h.mu.RLock()
	if !h.authTokens["token1"] {
		t.Error("expected token1 to be added")
	}
	h.mu.RUnlock()

	h.AddAuthToken("token2")
	h.mu.RLock()
	if len(h.authTokens) != 2 {
		t.Errorf("expected 2 tokens, got %d", len(h.authTokens))
	}
	h.mu.RUnlock()

	// Add empty token should be ignored
	h.AddAuthToken("")
	h.mu.RLock()
	if len(h.authTokens) != 2 {
		t.Errorf("expected 2 tokens (empty not added), got %d", len(h.authTokens))
	}
	h.mu.RUnlock()

	// Remove token
	h.RemoveAuthToken("token1")
	h.mu.RLock()
	if h.authTokens["token1"] {
		t.Error("expected token1 to be removed")
	}
	if len(h.authTokens) != 1 {
		t.Errorf("expected 1 token, got %d", len(h.authTokens))
	}
	h.mu.RUnlock()
}

func TestProfilingHandler_AuthMiddleware(t *testing.T) {
	h := NewProfilingHandler(ProfilingHandlerConfig{
		Logger: zap.NewNop(),
	})
	h.SetAuthTokens([]string{"valid-token"})

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	tests := []struct {
		name           string
		enabled        bool
		token          string
		expectedStatus int
		path           string
	}{
		{
			name:           "disabled handler returns 503",
			enabled:        false,
			token:          "valid-token",
			expectedStatus: http.StatusServiceUnavailable,
			path:           "/debug/pprof/",
		},
		{
			name:           "missing token returns 401",
			enabled:        true,
			token:          "",
			expectedStatus: http.StatusUnauthorized,
			path:           "/debug/pprof/",
		},
		{
			name:           "invalid token returns 401",
			enabled:        true,
			token:          "invalid-token",
			expectedStatus: http.StatusUnauthorized,
			path:           "/debug/pprof/",
		},
		{
			name:           "valid token returns 200",
			enabled:        true,
			token:          "valid-token",
			expectedStatus: http.StatusOK,
			path:           "/debug/pprof/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.enabled {
				h.Enable()
			} else {
				h.Disable()
			}

			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			if tt.token != "" {
				req.Header.Set("X-Admin-Token", tt.token)
			}
			rec := httptest.NewRecorder()

			mux.ServeHTTP(rec, req)

			if rec.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, rec.Code)
			}
		})
	}
}

func TestProfilingHandler_IndexEndpoint(t *testing.T) {
	h := NewProfilingHandler(ProfilingHandlerConfig{
		Logger: zap.NewNop(),
	})
	h.SetAuthTokens([]string{"test-token"})
	h.Enable()

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/debug/pprof/", nil)
	req.Header.Set("X-Admin-Token", "test-token")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	// The index page should contain pprof links
	body := rec.Body.String()
	if !strings.Contains(body, "pprof") {
		t.Error("expected index page to contain pprof links")
	}
}

func TestProfilingHandler_CmdlineEndpoint(t *testing.T) {
	h := NewProfilingHandler(ProfilingHandlerConfig{
		Logger: zap.NewNop(),
	})
	h.SetAuthTokens([]string{"test-token"})
	h.Enable()

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/debug/pprof/cmdline", nil)
	req.Header.Set("X-Admin-Token", "test-token")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}

func TestProfilingHandler_SymbolEndpoint(t *testing.T) {
	h := NewProfilingHandler(ProfilingHandlerConfig{
		Logger: zap.NewNop(),
	})
	h.SetAuthTokens([]string{"test-token"})
	h.Enable()

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/debug/pprof/symbol", nil)
	req.Header.Set("X-Admin-Token", "test-token")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}

func TestProfilingHandler_HeapEndpoint(t *testing.T) {
	h := NewProfilingHandler(ProfilingHandlerConfig{
		Logger: zap.NewNop(),
	})
	h.SetAuthTokens([]string{"test-token"})
	h.Enable()

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	tests := []struct {
		name string
		gc   string
	}{
		{
			name: "without gc",
			gc:   "",
		},
		{
			name: "with gc=1",
			gc:   "1",
		},
		{
			name: "with gc=true",
			gc:   "true",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := "/debug/pprof/heap"
			if tt.gc != "" {
				url += "?gc=" + tt.gc
			}

			req := httptest.NewRequest(http.MethodGet, url, nil)
			req.Header.Set("X-Admin-Token", "test-token")
			rec := httptest.NewRecorder()

			mux.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Errorf("expected status 200, got %d", rec.Code)
			}
		})
	}
}

func TestProfilingHandler_GoroutineEndpoint(t *testing.T) {
	h := NewProfilingHandler(ProfilingHandlerConfig{
		Logger: zap.NewNop(),
	})
	h.SetAuthTokens([]string{"test-token"})
	h.Enable()

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/debug/pprof/goroutine", nil)
	req.Header.Set("X-Admin-Token", "test-token")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}

func TestProfilingHandler_ThreadcreateEndpoint(t *testing.T) {
	h := NewProfilingHandler(ProfilingHandlerConfig{
		Logger: zap.NewNop(),
	})
	h.SetAuthTokens([]string{"test-token"})
	h.Enable()

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/debug/pprof/threadcreate", nil)
	req.Header.Set("X-Admin-Token", "test-token")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}

func TestProfilingHandler_BlockEndpoint(t *testing.T) {
	h := NewProfilingHandler(ProfilingHandlerConfig{
		Logger: zap.NewNop(),
	})
	h.SetAuthTokens([]string{"test-token"})
	h.Enable()

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/debug/pprof/block", nil)
	req.Header.Set("X-Admin-Token", "test-token")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}

func TestProfilingHandler_MutexEndpoint(t *testing.T) {
	h := NewProfilingHandler(ProfilingHandlerConfig{
		Logger: zap.NewNop(),
	})
	h.SetAuthTokens([]string{"test-token"})
	h.Enable()

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/debug/pprof/mutex", nil)
	req.Header.Set("X-Admin-Token", "test-token")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}

func TestProfilingHandler_ProfileEndpoint(t *testing.T) {
	h := NewProfilingHandler(ProfilingHandlerConfig{
		Logger:           zap.NewNop(),
		ProfileRateLimit: 5 * time.Second, // Rate limit longer than profile duration
	})
	h.SetAuthTokens([]string{"test-token"})
	h.Enable()

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	// First request should succeed
	req := httptest.NewRequest(http.MethodGet, "/debug/pprof/profile?seconds=1", nil)
	req.Header.Set("X-Admin-Token", "test-token")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	// Immediate second request should be rate limited (profile took 1s but rate limit is 5s)
	req2 := httptest.NewRequest(http.MethodGet, "/debug/pprof/profile?seconds=1", nil)
	req2.Header.Set("X-Admin-Token", "test-token")
	rec2 := httptest.NewRecorder()

	mux.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusTooManyRequests {
		t.Errorf("expected status 429 (rate limited), got %d", rec2.Code)
	}
}

func TestProfilingHandler_TraceEndpoint(t *testing.T) {
	h := NewProfilingHandler(ProfilingHandlerConfig{
		Logger:         zap.NewNop(),
		TraceRateLimit: 5 * time.Second, // Rate limit longer than trace duration
	})
	h.SetAuthTokens([]string{"test-token"})
	h.Enable()

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	// First request should succeed
	req := httptest.NewRequest(http.MethodGet, "/debug/pprof/trace?seconds=1", nil)
	req.Header.Set("X-Admin-Token", "test-token")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	// Immediate second request should be rate limited (trace took 1s but rate limit is 5s)
	req2 := httptest.NewRequest(http.MethodGet, "/debug/pprof/trace?seconds=1", nil)
	req2.Header.Set("X-Admin-Token", "test-token")
	rec2 := httptest.NewRecorder()

	mux.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusTooManyRequests {
		t.Errorf("expected status 429 (rate limited), got %d", rec2.Code)
	}
}

func TestProfilingHandler_MethodNotAllowed(t *testing.T) {
	h := NewProfilingHandler(ProfilingHandlerConfig{
		Logger: zap.NewNop(),
	})
	h.SetAuthTokens([]string{"test-token"})
	h.Enable()

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	endpoints := []string{
		"/debug/pprof/",
		"/debug/pprof/cmdline",
		"/debug/pprof/profile",
		"/debug/pprof/symbol",
		"/debug/pprof/trace",
		"/debug/pprof/heap",
		"/debug/pprof/goroutine",
		"/debug/pprof/threadcreate",
		"/debug/pprof/block",
		"/debug/pprof/mutex",
	}

	for _, endpoint := range endpoints {
		t.Run(endpoint, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, endpoint, nil)
			req.Header.Set("X-Admin-Token", "test-token")
			rec := httptest.NewRecorder()

			mux.ServeHTTP(rec, req)

			if rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("expected status 405 for POST to %s, got %d", endpoint, rec.Code)
			}
		})
	}
}

func TestParseDuration(t *testing.T) {
	tests := []struct {
		name            string
		input           string
		defaultDuration time.Duration
		maxDuration     time.Duration
		expected        time.Duration
	}{
		{
			name:            "empty string returns default",
			input:           "",
			defaultDuration: 30 * time.Second,
			maxDuration:     5 * time.Minute,
			expected:        30 * time.Second,
		},
		{
			name:            "valid duration",
			input:           "60",
			defaultDuration: 30 * time.Second,
			maxDuration:     5 * time.Minute,
			expected:        60 * time.Second,
		},
		{
			name:            "duration exceeds max",
			input:           "600",
			defaultDuration: 30 * time.Second,
			maxDuration:     5 * time.Minute,
			expected:        5 * time.Minute,
		},
		{
			name:            "zero returns default",
			input:           "0",
			defaultDuration: 30 * time.Second,
			maxDuration:     5 * time.Minute,
			expected:        30 * time.Second,
		},
		{
			name:            "negative returns default",
			input:           "-10",
			defaultDuration: 30 * time.Second,
			maxDuration:     5 * time.Minute,
			expected:        30 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseDuration(tt.input, tt.defaultDuration, tt.maxDuration)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestParseDuration_Invalid(t *testing.T) {
	_, err := parseDuration("invalid", 30*time.Second, 5*time.Minute)
	if err == nil {
		t.Error("expected error for invalid duration")
	}
}

func TestRateLimiter(t *testing.T) {
	rl := newRateLimiter(100 * time.Millisecond)

	// First call should succeed
	if !rl.Allow() {
		t.Error("expected first call to be allowed")
	}

	// Immediate second call should fail
	if rl.Allow() {
		t.Error("expected second call to be rate limited")
	}

	// Wait for rate limit to pass
	time.Sleep(150 * time.Millisecond)

	// Should succeed now
	if !rl.Allow() {
		t.Error("expected call after rate limit to be allowed")
	}
}

func TestProfilingHandler_GetProfileNames(t *testing.T) {
	h := NewProfilingHandler(ProfilingHandlerConfig{
		Logger: zap.NewNop(),
	})

	names := h.GetProfileNames()
	if len(names) == 0 {
		t.Error("expected at least one profile name")
	}

	// Common profiles that should always be available
	expectedProfiles := []string{"heap", "goroutine"}
	for _, expected := range expectedProfiles {
		found := false
		for _, name := range names {
			if name == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected to find profile %s", expected)
		}
	}
}

func TestProfilingHandler_RegisterRoutes(t *testing.T) {
	h := NewProfilingHandler(ProfilingHandlerConfig{
		Logger: zap.NewNop(),
	})
	h.SetAuthTokens([]string{"test-token"})
	h.Enable()

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	// Test that all expected routes are registered
	expectedRoutes := []string{
		"/debug/pprof/",
		"/debug/pprof/cmdline",
		"/debug/pprof/profile",
		"/debug/pprof/symbol",
		"/debug/pprof/trace",
		"/debug/pprof/heap",
		"/debug/pprof/goroutine",
		"/debug/pprof/threadcreate",
		"/debug/pprof/block",
		"/debug/pprof/mutex",
	}

	for _, route := range expectedRoutes {
		t.Run(route, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, route, nil)
			req.Header.Set("X-Admin-Token", "test-token")
			rec := httptest.NewRecorder()

			mux.ServeHTTP(rec, req)

			// We should not get 404
			if rec.Code == http.StatusNotFound {
				t.Errorf("route %s not registered", route)
			}
		})
	}
}

func TestProfilingHandler_ConcurrentAccess(t *testing.T) {
	h := NewProfilingHandler(ProfilingHandlerConfig{
		Logger: zap.NewNop(),
	})
	h.SetAuthTokens([]string{"test-token"})

	// Concurrent enable/disable
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				h.Enable()
				_ = h.IsEnabled()
				h.Disable()
				_ = h.IsEnabled()
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Concurrent token operations
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				h.SetAuthTokens([]string{"token1", "token2"})
				h.AddAuthToken("token3")
				h.RemoveAuthToken("token1")
			}
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}
}

func TestProfilingHandler_ProfileInvalidDuration(t *testing.T) {
	h := NewProfilingHandler(ProfilingHandlerConfig{
		Logger:           zap.NewNop(),
		ProfileRateLimit: 100 * time.Millisecond,
	})
	h.SetAuthTokens([]string{"test-token"})
	h.Enable()

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/debug/pprof/profile?seconds=invalid", nil)
	req.Header.Set("X-Admin-Token", "test-token")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400 for invalid duration, got %d", rec.Code)
	}
}

func TestProfilingHandler_TraceInvalidDuration(t *testing.T) {
	h := NewProfilingHandler(ProfilingHandlerConfig{
		Logger:         zap.NewNop(),
		TraceRateLimit: 100 * time.Millisecond,
	})
	h.SetAuthTokens([]string{"test-token"})
	h.Enable()

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/debug/pprof/trace?seconds=invalid", nil)
	req.Header.Set("X-Admin-Token", "test-token")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400 for invalid duration, got %d", rec.Code)
	}
}

func TestLookupProfile(t *testing.T) {
	profile := LookupProfile("heap")
	if profile == nil {
		t.Error("expected heap profile to exist")
	}

	profile = LookupProfile("nonexistent")
	if profile != nil {
		t.Error("expected nonexistent profile to be nil")
	}
}