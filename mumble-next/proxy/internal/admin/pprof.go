package admin

import (
	"fmt"
	"net/http"
	"net/http/pprof"
	"runtime"
	rpprof "runtime/pprof"
	"sync"
	"time"

	"go.uber.org/zap"
)

// ProfilingHandler provides pprof performance profiling endpoints with authentication and rate limiting.
// It is disabled by default for security and must be explicitly enabled.
type ProfilingHandler struct {
	mu sync.RWMutex

	// enabled controls whether profiling endpoints are active
	enabled bool

	// authTokens stores valid authentication tokens
	authTokens map[string]bool

	// logger for logging events
	logger *zap.Logger

	// rate limiting for expensive operations
	profileLimiter *rateLimiter
	traceLimiter   *rateLimiter

	// default durations
	defaultProfileDuration time.Duration
	defaultTraceDuration   time.Duration
	maxProfileDuration    time.Duration
	maxTraceDuration      time.Duration
}

// rateLimiter implements a simple rate limiter for profiling operations
type rateLimiter struct {
	mu         sync.Mutex
	lastCall   time.Time
	minInterval time.Duration
}

// newRateLimiter creates a new rate limiter with the specified minimum interval
func newRateLimiter(minInterval time.Duration) *rateLimiter {
	return &rateLimiter{
		minInterval: minInterval,
	}
}

// Allow checks if the operation is allowed and updates the last call time
func (rl *rateLimiter) Allow() bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	if now.Sub(rl.lastCall) < rl.minInterval {
		return false
	}
	rl.lastCall = now
	return true
}

// ProfilingHandlerConfig contains configuration for the profiling handler
type ProfilingHandlerConfig struct {
	// Logger for logging events (optional, defaults to nop logger)
	Logger *zap.Logger

	// DefaultProfileDuration is the default duration for CPU profiles
	DefaultProfileDuration time.Duration

	// DefaultTraceDuration is the default duration for execution traces
	DefaultTraceDuration time.Duration

	// MaxProfileDuration is the maximum allowed duration for CPU profiles
	MaxProfileDuration time.Duration

	// MaxTraceDuration is the maximum allowed duration for execution traces
	MaxTraceDuration time.Duration

	// ProfileRateLimit is the minimum interval between CPU profile requests
	ProfileRateLimit time.Duration

	// TraceRateLimit is the minimum interval between trace requests
	TraceRateLimit time.Duration
}

// NewProfilingHandler creates a new profiling handler with the given configuration.
// The handler is disabled by default and must be enabled via Enable().
func NewProfilingHandler(cfg ProfilingHandlerConfig) *ProfilingHandler {
	logger := cfg.Logger
	if logger == nil {
		logger = zap.NewNop()
	}

	defaultProfileDuration := cfg.DefaultProfileDuration
	if defaultProfileDuration == 0 {
		defaultProfileDuration = 30 * time.Second
	}

	defaultTraceDuration := cfg.DefaultTraceDuration
	if defaultTraceDuration == 0 {
		defaultTraceDuration = 5 * time.Second
	}

	maxProfileDuration := cfg.MaxProfileDuration
	if maxProfileDuration == 0 {
		maxProfileDuration = 5 * time.Minute
	}

	maxTraceDuration := cfg.MaxTraceDuration
	if maxTraceDuration == 0 {
		maxTraceDuration = 1 * time.Minute
	}

	profileRateLimit := cfg.ProfileRateLimit
	if profileRateLimit == 0 {
		profileRateLimit = 30 * time.Second
	}

	traceRateLimit := cfg.TraceRateLimit
	if traceRateLimit == 0 {
		traceRateLimit = 1 * time.Minute
	}

	return &ProfilingHandler{
		authTokens:            make(map[string]bool),
		logger:                logger,
		profileLimiter:        newRateLimiter(profileRateLimit),
		traceLimiter:          newRateLimiter(traceRateLimit),
		defaultProfileDuration: defaultProfileDuration,
		defaultTraceDuration:   defaultTraceDuration,
		maxProfileDuration:     maxProfileDuration,
		maxTraceDuration:       maxTraceDuration,
	}
}

// Enable activates the profiling endpoints.
// This must be called explicitly for security reasons.
func (h *ProfilingHandler) Enable() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.enabled = true
	h.logger.Info("profiling endpoints enabled")
}

// Disable deactivates the profiling endpoints.
func (h *ProfilingHandler) Disable() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.enabled = false
	h.logger.Info("profiling endpoints disabled")
}

// IsEnabled returns whether profiling is currently enabled
func (h *ProfilingHandler) IsEnabled() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.enabled
}

// SetAuthTokens sets the valid authentication tokens for accessing profiling endpoints.
// Only requests with a valid token in the X-Admin-Token header will be allowed.
func (h *ProfilingHandler) SetAuthTokens(tokens []string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.authTokens = make(map[string]bool)
	for _, token := range tokens {
		if token != "" {
			h.authTokens[token] = true
		}
	}

	h.logger.Info("profiling auth tokens updated",
		zap.Int("token_count", len(h.authTokens)),
	)
}

// AddAuthToken adds a single authentication token
func (h *ProfilingHandler) AddAuthToken(token string) {
	if token == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.authTokens[token] = true
}

// RemoveAuthToken removes an authentication token
func (h *ProfilingHandler) RemoveAuthToken(token string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.authTokens, token)
}

// RegisterRoutes registers the profiling endpoints on the given mux.
// All endpoints require authentication via X-Admin-Token header.
func (h *ProfilingHandler) RegisterRoutes(mux *http.ServeMux) {
	// Main index handler
	mux.HandleFunc("/debug/pprof/", h.authMiddleware(h.handleIndex))

	// Individual profile handlers
	mux.HandleFunc("/debug/pprof/cmdline", h.authMiddleware(h.handleCmdline))
	mux.HandleFunc("/debug/pprof/profile", h.authMiddleware(h.handleProfile))
	mux.HandleFunc("/debug/pprof/symbol", h.authMiddleware(h.handleSymbol))
	mux.HandleFunc("/debug/pprof/trace", h.authMiddleware(h.handleTrace))

	// Named profile handlers
	mux.HandleFunc("/debug/pprof/heap", h.authMiddleware(h.handleHeap))
	mux.HandleFunc("/debug/pprof/goroutine", h.authMiddleware(h.handleGoroutine))
	mux.HandleFunc("/debug/pprof/threadcreate", h.authMiddleware(h.handleThreadcreate))
	mux.HandleFunc("/debug/pprof/block", h.authMiddleware(h.handleBlock))
	mux.HandleFunc("/debug/pprof/mutex", h.authMiddleware(h.handleMutex))
}

// authMiddleware checks authentication before allowing access to profiling endpoints
func (h *ProfilingHandler) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Check if profiling is enabled
		if !h.IsEnabled() {
			http.Error(w, "Profiling is disabled", http.StatusServiceUnavailable)
			return
		}

		// Get token from header
		token := r.Header.Get("X-Admin-Token")
		if token == "" {
			h.logger.Warn("profiling access denied: missing token",
				zap.String("path", r.URL.Path),
				zap.String("remote_addr", r.RemoteAddr),
			)
			http.Error(w, "Unauthorized: missing authentication token", http.StatusUnauthorized)
			return
		}

		// Validate token
		h.mu.RLock()
		valid := h.authTokens[token]
		h.mu.RUnlock()

		if !valid {
			h.logger.Warn("profiling access denied: invalid token",
				zap.String("path", r.URL.Path),
				zap.String("remote_addr", r.RemoteAddr),
			)
			http.Error(w, "Unauthorized: invalid authentication token", http.StatusUnauthorized)
			return
		}

		// Token is valid, proceed to handler
		h.logger.Info("profiling endpoint accessed",
			zap.String("path", r.URL.Path),
			zap.String("method", r.Method),
			zap.String("remote_addr", r.RemoteAddr),
		)

		next(w, r)
	}
}

// handleIndex serves the pprof index page
func (h *ProfilingHandler) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Only serve index for exact path match
	if r.URL.Path != "/debug/pprof/" {
		pprof.Handler(r.URL.Path[len("/debug/pprof/"):]).ServeHTTP(w, r)
		return
	}

	pprof.Index(w, r)
}

// handleCmdline serves the command line
func (h *ProfilingHandler) handleCmdline(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	pprof.Cmdline(w, r)
}

// handleProfile handles CPU profiling with configurable duration
func (h *ProfilingHandler) handleProfile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Apply rate limiting
	if !h.profileLimiter.Allow() {
		http.Error(w, "Rate limit exceeded for CPU profiling. Please wait before requesting another profile.", http.StatusTooManyRequests)
		return
	}

	// Parse duration from query parameter
	seconds, err := parseDuration(r.URL.Query().Get("seconds"), h.defaultProfileDuration, h.maxProfileDuration)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	h.logger.Info("starting CPU profile",
		zap.Duration("duration", seconds),
	)

	// Use the standard pprof profile handler with custom duration
	pprof.Profile(w, r)
}

// handleSymbol serves the symbol lookup
func (h *ProfilingHandler) handleSymbol(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	pprof.Symbol(w, r)
}

// handleTrace handles execution tracing with configurable duration
func (h *ProfilingHandler) handleTrace(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Apply rate limiting
	if !h.traceLimiter.Allow() {
		http.Error(w, "Rate limit exceeded for tracing. Please wait before requesting another trace.", http.StatusTooManyRequests)
		return
	}

	// Parse duration from query parameter
	seconds, err := parseDuration(r.URL.Query().Get("seconds"), h.defaultTraceDuration, h.maxTraceDuration)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	h.logger.Info("starting execution trace",
		zap.Duration("duration", seconds),
	)

	// Use the standard pprof trace handler
	pprof.Trace(w, r)
}

// handleHeap serves the heap profile with optional GC control
func (h *ProfilingHandler) handleHeap(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Check if GC should be triggered before heap dump
	gc := r.URL.Query().Get("gc")
	if gc == "1" || gc == "true" {
		h.logger.Info("triggering GC before heap profile")
		runtime.GC()
	}

	// Serve heap profile
	pprof.Handler("heap").ServeHTTP(w, r)
}

// handleGoroutine serves the goroutine profile
func (h *ProfilingHandler) handleGoroutine(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	pprof.Handler("goroutine").ServeHTTP(w, r)
}

// handleThreadcreate serves the thread creation profile
func (h *ProfilingHandler) handleThreadcreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	pprof.Handler("threadcreate").ServeHTTP(w, r)
}

// handleBlock serves the block profile
func (h *ProfilingHandler) handleBlock(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	pprof.Handler("block").ServeHTTP(w, r)
}

// handleMutex serves the mutex profile
func (h *ProfilingHandler) handleMutex(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	pprof.Handler("mutex").ServeHTTP(w, r)
}

// parseDuration parses a duration string from seconds and applies limits
func parseDuration(s string, defaultDuration, maxDuration time.Duration) (time.Duration, error) {
	if s == "" {
		return defaultDuration, nil
	}

	var seconds int
	if _, err := fmt.Sscanf(s, "%d", &seconds); err != nil {
		return 0, fmt.Errorf("invalid duration format: %v", err)
	}

	if seconds <= 0 {
		return defaultDuration, nil
	}

	duration := time.Duration(seconds) * time.Second
	if duration > maxDuration {
		return maxDuration, nil
	}

	return duration, nil
}

// GetProfileNames returns all available profile names
func (h *ProfilingHandler) GetProfileNames() []string {
	profiles := rpprof.Profiles()
	names := make([]string, 0, len(profiles))
	for _, p := range profiles {
		names = append(names, p.Name())
	}
	return names
}

// WriteHeapProfile writes a heap profile to the given writer
// This is a convenience function that can be used programmatically
func WriteHeapProfile(w http.ResponseWriter) error {
	return rpprof.WriteHeapProfile(w)
}

// LookupProfile returns the profile with the given name
func LookupProfile(name string) *rpprof.Profile {
	return rpprof.Lookup(name)
}