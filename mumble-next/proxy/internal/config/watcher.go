package config

import (
	"context"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"
	"go.uber.org/zap"
)

// ConfigWatcher provides hot reload capabilities for configuration files.
// It watches the file system for changes and reloads configuration atomically.
type ConfigWatcher struct {
	configPath string
	config     atomic.Pointer[Config]
	logger     *zap.Logger

	mu           sync.RWMutex
	changeCbs    []func(old, new *Config)
	errorCbs     []func(error)
	watcher      *fsnotify.Watcher
	debounceTimer *time.Timer
	debounceMu   sync.Mutex
	debounceTime time.Duration

	closed  atomic.Bool
	closeCh chan struct{}

	// Non-reloadable fields tracking
	nonReloadableFields NonReloadableFields
}

// NonReloadableFields tracks configuration fields that require restart
type NonReloadableFields struct {
	ServerHost string
	ServerPort int
	TLSEnabled bool
	TLSCert    string
	TLSKey     string
	MetricsPort int
}

// ConfigWatcherOption is a functional option for ConfigWatcher
type ConfigWatcherOption func(*ConfigWatcher)

// WithDebounce sets the debounce duration for file change events
func WithDebounce(d time.Duration) ConfigWatcherOption {
	return func(w *ConfigWatcher) {
		w.debounceTime = d
	}
}

// WithLogger sets the logger for the watcher
func WithLogger(logger *zap.Logger) ConfigWatcherOption {
	return func(w *ConfigWatcher) {
		w.logger = logger
	}
}

// NewConfigWatcher creates a new configuration watcher
func NewConfigWatcher(configPath string, initialConfig *Config, opts ...ConfigWatcherOption) (*ConfigWatcher, error) {
	if initialConfig == nil {
		return nil, fmt.Errorf("initial config cannot be nil")
	}

	w := &ConfigWatcher{
		configPath:    configPath,
		logger:        zap.NewNop(),
		debounceTime:  500 * time.Millisecond,
		changeCbs:     make([]func(old, new *Config), 0),
		errorCbs:      make([]func(error), 0),
		closeCh:       make(chan struct{}),
	}

	// Apply options
	for _, opt := range opts {
		opt(w)
	}

	// Store initial config
	w.config.Store(initialConfig)

	// Track non-reloadable fields
	w.nonReloadableFields = NonReloadableFields{
		ServerHost:  initialConfig.Server.Host,
		ServerPort:  initialConfig.Server.Port,
		TLSEnabled:  initialConfig.Server.TLS.Enabled,
		TLSCert:     initialConfig.Server.TLS.CertFile,
		TLSKey:      initialConfig.Server.TLS.KeyFile,
		MetricsPort: initialConfig.Metrics.Port,
	}

	return w, nil
}

// Watch starts watching the configuration file for changes.
// It blocks until the context is cancelled or an unrecoverable error occurs.
func (w *ConfigWatcher) Watch(ctx context.Context) error {
	// Create fsnotify watcher
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("failed to create file watcher: %w", err)
	}
	w.mu.Lock()
	w.watcher = watcher
	w.mu.Unlock()

	// Add the config file to the watcher
	if err := w.watcher.Add(w.configPath); err != nil {
		w.watcher.Close()
		return fmt.Errorf("failed to watch config file %s: %w", w.configPath, err)
	}

	w.logger.Info("started watching configuration file",
		zap.String("path", w.configPath),
		zap.Duration("debounce", w.debounceTime),
	)

	// Main event loop
	for {
		select {
		case <-ctx.Done():
			w.logger.Info("stopping config watcher: context cancelled")
			return ctx.Err()

		case <-w.closeCh:
			w.logger.Info("stopping config watcher: close requested")
			return nil

		case event, ok := <-w.watcher.Events:
			if !ok {
				return fmt.Errorf("watcher events channel closed")
			}
			w.handleEvent(event)

		case err, ok := <-w.watcher.Errors:
			if !ok {
				return fmt.Errorf("watcher errors channel closed")
			}
			w.notifyError(fmt.Errorf("watcher error: %w", err))
		}
	}
}

// handleEvent processes file system events
func (w *ConfigWatcher) handleEvent(event fsnotify.Event) {
	// Only handle write and create events
	if !event.Has(fsnotify.Write) && !event.Has(fsnotify.Create) {
		return
	}

	w.logger.Debug("config file event",
		zap.String("event", event.String()),
		zap.String("path", event.Name),
	)

	// Debounce rapid file changes
	w.debounceMu.Lock()
	if w.debounceTimer != nil {
		w.debounceTimer.Stop()
	}
	w.debounceTimer = time.AfterFunc(w.debounceTime, func() {
		w.debounceMu.Lock()
		w.debounceTimer = nil
		w.debounceMu.Unlock()
		w.triggerReload()
	})
	w.debounceMu.Unlock()
}

// triggerReload performs the actual configuration reload
func (w *ConfigWatcher) triggerReload() {
	if w.closed.Load() {
		return
	}

	w.logger.Info("triggering configuration reload")

	if err := w.Reload(); err != nil {
		w.notifyError(fmt.Errorf("configuration reload failed: %w", err))
	}
}

// Reload manually triggers a configuration reload.
// This can be called programmatically or triggered by file changes.
func (w *ConfigWatcher) Reload() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed.Load() {
		return fmt.Errorf("watcher is closed")
	}

	// Load new config
	newConfig, err := Load(w.configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Validate new config
	if err := newConfig.Validate(); err != nil {
		return fmt.Errorf("config validation failed: %w", err)
	}

	// Check non-reloadable fields
	if err := w.checkNonReloadableFields(newConfig); err != nil {
		w.logger.Warn("non-reloadable fields changed, these changes require restart",
			zap.Error(err),
		)
		// Continue with reload for reloadable fields
		// but preserve non-reloadable fields
		w.preserveNonReloadableFields(newConfig)
	}

	// Get old config
	oldConfig := w.config.Load()

	// Atomic swap
	w.config.Store(newConfig)

	w.logger.Info("configuration reloaded successfully",
		zap.String("murmur_host", newConfig.Murmur.Host),
		zap.Int("murmur_port", newConfig.Murmur.Port),
		zap.Int("backends_count", len(newConfig.Backends)),
		zap.String("log_level", newConfig.Logging.Level),
	)

	// Notify change callbacks
	w.notifyChange(oldConfig, newConfig)

	return nil
}

// checkNonReloadableFields verifies that non-reloadable fields haven't changed
func (w *ConfigWatcher) checkNonReloadableFields(newConfig *Config) error {
	var changes []string

	if newConfig.Server.Host != w.nonReloadableFields.ServerHost {
		changes = append(changes, fmt.Sprintf("server.host: %s -> %s", w.nonReloadableFields.ServerHost, newConfig.Server.Host))
	}
	if newConfig.Server.Port != w.nonReloadableFields.ServerPort {
		changes = append(changes, fmt.Sprintf("server.port: %d -> %d", w.nonReloadableFields.ServerPort, newConfig.Server.Port))
	}
	if newConfig.Server.TLS.Enabled != w.nonReloadableFields.TLSEnabled {
		changes = append(changes, fmt.Sprintf("server.tls.enabled: %v -> %v", w.nonReloadableFields.TLSEnabled, newConfig.Server.TLS.Enabled))
	}
	if newConfig.Server.TLS.CertFile != w.nonReloadableFields.TLSCert {
		changes = append(changes, fmt.Sprintf("server.tls.cert_file: %s -> %s", w.nonReloadableFields.TLSCert, newConfig.Server.TLS.CertFile))
	}
	if newConfig.Server.TLS.KeyFile != w.nonReloadableFields.TLSKey {
		changes = append(changes, fmt.Sprintf("server.tls.key_file: %s -> %s", w.nonReloadableFields.TLSKey, newConfig.Server.TLS.KeyFile))
	}
	if newConfig.Metrics.Port != w.nonReloadableFields.MetricsPort {
		changes = append(changes, fmt.Sprintf("metrics.port: %d -> %d", w.nonReloadableFields.MetricsPort, newConfig.Metrics.Port))
	}

	if len(changes) > 0 {
		return fmt.Errorf("non-reloadable fields changed: %v", changes)
	}

	return nil
}

// preserveNonReloadableFields restores non-reloadable fields to their original values
func (w *ConfigWatcher) preserveNonReloadableFields(newConfig *Config) {
	newConfig.Server.Host = w.nonReloadableFields.ServerHost
	newConfig.Server.Port = w.nonReloadableFields.ServerPort
	newConfig.Server.TLS.Enabled = w.nonReloadableFields.TLSEnabled
	newConfig.Server.TLS.CertFile = w.nonReloadableFields.TLSCert
	newConfig.Server.TLS.KeyFile = w.nonReloadableFields.TLSKey
	newConfig.Metrics.Port = w.nonReloadableFields.MetricsPort
}

// notifyChange calls all registered change callbacks
func (w *ConfigWatcher) notifyChange(oldConfig, newConfig *Config) {
	for _, cb := range w.changeCbs {
		// Run callbacks in goroutines to prevent blocking
		go func(callback func(old, new *Config)) {
			defer func() {
				if r := recover(); r != nil {
					w.logger.Error("panic in change callback",
						zap.Any("panic", r),
					)
				}
			}()
			callback(oldConfig, newConfig)
		}(cb)
	}
}

// notifyError calls all registered error callbacks
func (w *ConfigWatcher) notifyError(err error) {
	for _, cb := range w.errorCbs {
		go func(callback func(error)) {
			defer func() {
				if r := recover(); r != nil {
					w.logger.Error("panic in error callback",
						zap.Any("panic", r),
					)
				}
			}()
			callback(err)
		}(cb)
	}
}

// OnChange registers a callback that is called when the configuration changes.
// Multiple callbacks can be registered and they are called concurrently.
func (w *ConfigWatcher) OnChange(callback func(old, new *Config)) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.changeCbs = append(w.changeCbs, callback)
}

// OnError registers a callback that is called when an error occurs.
// Multiple callbacks can be registered.
func (w *ConfigWatcher) OnError(callback func(error)) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.errorCbs = append(w.errorCbs, callback)
}

// GetConfig returns the current configuration atomically.
func (w *ConfigWatcher) GetConfig() *Config {
	return w.config.Load()
}

// Close stops the watcher and releases resources.
func (w *ConfigWatcher) Close() error {
	if !w.closed.CompareAndSwap(false, true) {
		return nil // Already closed
	}

	// Stop debounce timer
	w.debounceMu.Lock()
	if w.debounceTimer != nil {
		w.debounceTimer.Stop()
		w.debounceTimer = nil
	}
	w.debounceMu.Unlock()

	// Signal close
	close(w.closeCh)

	// Close fsnotify watcher
	w.mu.RLock()
	if w.watcher != nil {
		w.watcher.Close()
	}
	w.mu.RUnlock()

	w.logger.Info("config watcher closed")
	return nil
}

// IsReloadableSection checks if a configuration section can be hot-reloaded
func IsReloadableSection(section string) bool {
	reloadable := map[string]bool{
		"backends":      true,
		"auth":         true,
		"auth_tokens":   true,
		"limits":       true,
		"rate_limits":  true,
		"logging":      true,
		"ip_filter":    true,
		"ip_whitelist": true,
		"ip_blacklist": true,
	}
	return reloadable[section]
}

// RequiresRestart checks if the configuration change requires a restart
func RequiresRestart(old, new *Config) []string {
	var reasons []string

	if old.Server.Host != new.Server.Host {
		reasons = append(reasons, "server host changed")
	}
	if old.Server.Port != new.Server.Port {
		reasons = append(reasons, "server port changed")
	}
	if old.Server.TLS.Enabled != new.Server.TLS.Enabled {
		reasons = append(reasons, "TLS enabled/disabled")
	}
	if old.Server.TLS.CertFile != new.Server.TLS.CertFile {
		reasons = append(reasons, "TLS certificate file changed")
	}
	if old.Server.TLS.KeyFile != new.Server.TLS.KeyFile {
		reasons = append(reasons, "TLS key file changed")
	}
	if old.Metrics.Port != new.Metrics.Port {
		reasons = append(reasons, "metrics port changed")
	}

	return reasons
}

// GetChangedSections returns which sections of the config have changed
func GetChangedSections(old, new *Config) []string {
	var changed []string

	// Check Backends
	if !backendsEqual(old.Backends, new.Backends) {
		changed = append(changed, "backends")
	}

	// Check Auth
	if old.Auth.Enabled != new.Auth.Enabled ||
		old.Auth.HeaderName != new.Auth.HeaderName ||
		old.Auth.QueryParam != new.Auth.QueryParam {
		changed = append(changed, "auth")
	}

	// Check Limits (rate limits)
	if old.Limits.MaxConnections != new.Limits.MaxConnections ||
		old.Limits.MaxConnectionsPerIP != new.Limits.MaxConnectionsPerIP ||
		old.Limits.ConnectionTimeout != new.Limits.ConnectionTimeout {
		changed = append(changed, "limits")
	}

	// Check Logging
	if old.Logging.Level != new.Logging.Level ||
		old.Logging.Format != new.Logging.Format {
		changed = append(changed, "logging")
	}

	// Check Server (non-reloadable)
	if old.Server.Host != new.Server.Host ||
		old.Server.Port != new.Server.Port {
		changed = append(changed, "server")
	}

	// Check TLS (non-reloadable)
	if old.Server.TLS.Enabled != new.Server.TLS.Enabled ||
		old.Server.TLS.CertFile != new.Server.TLS.CertFile ||
		old.Server.TLS.KeyFile != new.Server.TLS.KeyFile {
		changed = append(changed, "tls")
	}

	// Check Metrics (non-reloadable port)
	if old.Metrics.Port != new.Metrics.Port {
		changed = append(changed, "metrics")
	}

	return changed
}

// backendsEqual compares two backend configurations
func backendsEqual(a, b []BackendConfig) bool {
	if len(a) != len(b) {
		return false
	}

	aMap := make(map[string]BackendConfig)
	for _, backend := range a {
		aMap[backend.Name] = backend
	}

	for _, backend := range b {
		backendA, exists := aMap[backend.Name]
		if !exists {
			return false
		}
		if backendA.Host != backend.Host ||
			backendA.Port != backend.Port ||
			!tokensEqual(backendA.Tokens, backend.Tokens) {
			return false
		}
	}

	return true
}

// tokensEqual compares two token slices
func tokensEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	aSet := make(map[string]bool)
	for _, t := range a {
		aSet[t] = true
	}
	for _, t := range b {
		if !aSet[t] {
			return false
		}
	}
	return true
}

// IPFilterConfig represents IP whitelist/blacklist configuration
type IPFilterConfig struct {
	Whitelist []string `mapstructure:"whitelist"`
	Blacklist []string `mapstructure:"blacklist"`
	Enabled   bool     `mapstructure:"enabled"`
}

// ParseIPFilter parses IP filter configuration
func ParseIPFilter(whitelist, blacklist []string) ([]net.IPNet, []net.IPNet, error) {
	var whitelistNets []net.IPNet
	var blacklistNets []net.IPNet

	for _, cidr := range whitelist {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err != nil {
			// Try parsing as single IP
			ip := net.ParseIP(cidr)
			if ip == nil {
				return nil, nil, fmt.Errorf("invalid whitelist CIDR/IP: %s", cidr)
			}
			// Convert to /32 or /128 CIDR
			if ip.To4() != nil {
				_, ipNet, _ = net.ParseCIDR(ip.String() + "/32")
			} else {
				_, ipNet, _ = net.ParseCIDR(ip.String() + "/128")
			}
		}
		whitelistNets = append(whitelistNets, *ipNet)
	}

	for _, cidr := range blacklist {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err != nil {
			// Try parsing as single IP
			ip := net.ParseIP(cidr)
			if ip == nil {
				return nil, nil, fmt.Errorf("invalid blacklist CIDR/IP: %s", cidr)
			}
			// Convert to /32 or /128 CIDR
			if ip.To4() != nil {
				_, ipNet, _ = net.ParseCIDR(ip.String() + "/32")
			} else {
				_, ipNet, _ = net.ParseCIDR(ip.String() + "/128")
			}
		}
		blacklistNets = append(blacklistNets, *ipNet)
	}

	return whitelistNets, blacklistNets, nil
}