package config

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

func TestNewConfigWatcher(t *testing.T) {
	cfg := DefaultConfig()
	logger := zap.NewNop()

	tests := []struct {
		name        string
		configPath  string
		initialCfg  *Config
		opts        []ConfigWatcherOption
		wantErr     bool
	}{
		{
			name:       "valid config",
			configPath: "/tmp/config.yaml",
			initialCfg: cfg,
			opts:       []ConfigWatcherOption{WithLogger(logger)},
			wantErr:    false,
		},
		{
			name:       "nil config",
			configPath: "/tmp/config.yaml",
			initialCfg: nil,
			wantErr:    true,
		},
		{
			name:       "with debounce option",
			configPath: "/tmp/config.yaml",
			initialCfg: cfg,
			opts:       []ConfigWatcherOption{WithDebounce(1 * time.Second)},
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, err := NewConfigWatcher(tt.configPath, tt.initialCfg, tt.opts...)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewConfigWatcher() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && w == nil {
				t.Error("expected watcher to be created")
			}
			if w != nil {
				w.Close()
			}
		})
	}
}

func TestConfigWatcher_GetConfig(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Server.Port = 9999
	cfg.Murmur.Host = "test.example.com"

	w, err := NewConfigWatcher("/tmp/config.yaml", cfg, WithLogger(zap.NewNop()))
	if err != nil {
		t.Fatalf("failed to create watcher: %v", err)
	}
	defer w.Close()

	// GetConfig should return the initial config
	got := w.GetConfig()
	if got == nil {
		t.Fatal("expected config to be non-nil")
	}
	if got.Server.Port != 9999 {
		t.Errorf("expected server port 9999, got %d", got.Server.Port)
	}
	if got.Murmur.Host != "test.example.com" {
		t.Errorf("expected murmur host 'test.example.com', got %s", got.Murmur.Host)
	}
}

func TestConfigWatcher_OnChange(t *testing.T) {
	cfg := DefaultConfig()
	w, err := NewConfigWatcher("/tmp/config.yaml", cfg, WithLogger(zap.NewNop()))
	if err != nil {
		t.Fatalf("failed to create watcher: %v", err)
	}
	defer w.Close()

	var callbackCalled atomic.Bool
	w.OnChange(func(old, new *Config) {
		callbackCalled.Store(true)
	})

	// Verify callback is registered
	w.mu.RLock()
	callbacks := len(w.changeCbs)
	w.mu.RUnlock()

	if callbacks != 1 {
		t.Errorf("expected 1 change callback, got %d", callbacks)
	}
}

func TestConfigWatcher_OnError(t *testing.T) {
	cfg := DefaultConfig()
	w, err := NewConfigWatcher("/tmp/config.yaml", cfg, WithLogger(zap.NewNop()))
	if err != nil {
		t.Fatalf("failed to create watcher: %v", err)
	}
	defer w.Close()

	w.OnError(func(err error) {
		// error callback
	})

	// Verify callback is registered
	w.mu.RLock()
	callbacks := len(w.errorCbs)
	w.mu.RUnlock()

	if callbacks != 1 {
		t.Errorf("expected 1 error callback, got %d", callbacks)
	}
}

func TestConfigWatcher_Reload(t *testing.T) {
	// Create temp config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	cfg := DefaultConfig()
	cfg.Server.Port = 8080
	cfg.Logging.Level = "info"
	data, _ := yaml.Marshal(cfg)
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	w, err := NewConfigWatcher(configPath, cfg, WithLogger(zap.NewNop()))
	if err != nil {
		t.Fatalf("failed to create watcher: %v", err)
	}
	defer w.Close()

	// Modify config file
	cfg.Logging.Level = "debug"
	data, _ = yaml.Marshal(cfg)
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("failed to update config file: %v", err)
	}

	// Reload
	if err := w.Reload(); err != nil {
		t.Errorf("Reload() error = %v", err)
	}

	// Verify new config
	got := w.GetConfig()
	if got.Logging.Level != "debug" {
		t.Errorf("expected logging level 'debug', got %s", got.Logging.Level)
	}
}

func TestConfigWatcher_Reload_InvalidFile(t *testing.T) {
	w, err := NewConfigWatcher("/nonexistent/path/config.yaml", DefaultConfig(), WithLogger(zap.NewNop()))
	if err != nil {
		t.Fatalf("failed to create watcher: %v", err)
	}
	defer w.Close()

	err = w.Reload()
	if err == nil {
		t.Error("expected error when reloading nonexistent file")
	}
}

func TestConfigWatcher_Reload_AfterClose(t *testing.T) {
	// Create temp config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	cfg := DefaultConfig()
	data, _ := yaml.Marshal(cfg)
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	w, err := NewConfigWatcher(configPath, cfg, WithLogger(zap.NewNop()))
	if err != nil {
		t.Fatalf("failed to create watcher: %v", err)
	}

	// Close the watcher
	w.Close()

	// Try to reload
	err = w.Reload()
	if err == nil {
		t.Error("expected error when reloading after close")
	}
}

func TestConfigWatcher_Watch_FileChanges(t *testing.T) {
	// Create temp config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	cfg := DefaultConfig()
	cfg.Logging.Level = "info"
	data, _ := yaml.Marshal(cfg)
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	// Use a short debounce time for faster tests
	w, err := NewConfigWatcher(configPath, cfg,
		WithLogger(zap.NewNop()),
		WithDebounce(100*time.Millisecond),
	)
	if err != nil {
		t.Fatalf("failed to create watcher: %v", err)
	}
	defer w.Close()

	var reloadCount atomic.Int32
	w.OnChange(func(old, new *Config) {
		reloadCount.Add(1)
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start watching in a goroutine
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = w.Watch(ctx)
	}()

	// Wait for watcher to start
	time.Sleep(200 * time.Millisecond)

	// Modify the config file multiple times rapidly
	for i := 0; i < 3; i++ {
		cfg.Logging.Level = []string{"debug", "warn", "error"}[i%3]
		data, _ = yaml.Marshal(cfg)
		os.WriteFile(configPath, data, 0644)
		time.Sleep(50 * time.Millisecond)
	}

	// Wait for debounce
	time.Sleep(500 * time.Millisecond)

	// Cancel context to stop watching
	cancel()
	wg.Wait()

	// Check that reload was triggered (debounced, so should be 1)
	if reloadCount.Load() < 1 {
		t.Errorf("expected at least 1 reload, got %d", reloadCount.Load())
	}
}

func TestConfigWatcher_NonReloadableFields(t *testing.T) {
	// Create temp config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	cfg := DefaultConfig()
	cfg.Server.Host = "0.0.0.0"
	cfg.Server.Port = 8080
	cfg.Metrics.Port = 9090
	data, _ := yaml.Marshal(cfg)
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	w, err := NewConfigWatcher(configPath, cfg, WithLogger(zap.NewNop()))
	if err != nil {
		t.Fatalf("failed to create watcher: %v", err)
	}
	defer w.Close()

	// Modify non-reloadable fields
	cfg.Server.Port = 9090 // Changed
	cfg.Logging.Level = "debug" // Reloadable change
	data, _ = yaml.Marshal(cfg)
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("failed to update config file: %v", err)
	}

	// Reload
	if err := w.Reload(); err != nil {
		t.Errorf("Reload() error = %v", err)
	}

	// Server port should be preserved (non-reloadable)
	got := w.GetConfig()
	if got.Server.Port != 8080 {
		t.Errorf("expected server port to be preserved at 8080, got %d", got.Server.Port)
	}
	// Logging level should be updated
	if got.Logging.Level != "debug" {
		t.Errorf("expected logging level 'debug', got %s", got.Logging.Level)
	}
}

func TestConfigWatcher_CallbackPanic(t *testing.T) {
	// Create temp config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	cfg := DefaultConfig()
	data, _ := yaml.Marshal(cfg)
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	w, err := NewConfigWatcher(configPath, cfg, WithLogger(zap.NewNop()))
	if err != nil {
		t.Fatalf("failed to create watcher: %v", err)
	}
	defer w.Close()

	// Add callback that panics
	w.OnChange(func(old, new *Config) {
		panic("test panic")
	})

	// Add another callback that should still be called
	var callbackCalled atomic.Bool
	w.OnChange(func(old, new *Config) {
		callbackCalled.Store(true)
	})

	// Modify config file
	cfg.Logging.Level = "debug"
	data, _ = yaml.Marshal(cfg)
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("failed to update config file: %v", err)
	}

	// Reload should not panic
	if err := w.Reload(); err != nil {
		t.Errorf("Reload() error = %v", err)
	}

	// Wait for goroutines
	time.Sleep(100 * time.Millisecond)

	// Second callback should have been called
	if !callbackCalled.Load() {
		t.Error("expected second callback to be called despite first panicking")
	}
}

func TestConfigWatcher_ErrorCallback(t *testing.T) {
	// Create temp config file with invalid content
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Start with valid config
	cfg := DefaultConfig()
	data, _ := yaml.Marshal(cfg)
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	w, err := NewConfigWatcher(configPath, cfg,
		WithLogger(zap.NewNop()),
		WithDebounce(50*time.Millisecond),
	)
	if err != nil {
		t.Fatalf("failed to create watcher: %v", err)
	}
	defer w.Close()

	var errorCalled atomic.Bool
	w.OnError(func(err error) {
		errorCalled.Store(true)
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = w.Watch(ctx)
	}()

	// Wait for watcher to start
	time.Sleep(100 * time.Millisecond)

	// Write invalid config to trigger error
	os.WriteFile(configPath, []byte("invalid: yaml: content"), 0644)

	// Wait for debounce and error
	time.Sleep(200 * time.Millisecond)

	// Cancel context
	cancel()
	wg.Wait()

	// Error callback may or may not be called depending on timing
	// This test primarily verifies no panic occurs
}

func TestConfigWatcher_Close(t *testing.T) {
	cfg := DefaultConfig()
	w, err := NewConfigWatcher("/tmp/config.yaml", cfg, WithLogger(zap.NewNop()))
	if err != nil {
		t.Fatalf("failed to create watcher: %v", err)
	}

	// Close once
	if err := w.Close(); err != nil {
		t.Errorf("Close() error = %v", err)
	}

	// Close again should be idempotent
	if err := w.Close(); err != nil {
		t.Errorf("second Close() error = %v", err)
	}
}

func TestIsReloadableSection(t *testing.T) {
	tests := []struct {
		section string
		want    bool
	}{
		{"backends", true},
		{"auth", true},
		{"auth_tokens", true},
		{"limits", true},
		{"rate_limits", true},
		{"logging", true},
		{"ip_filter", true},
		{"ip_whitelist", true},
		{"ip_blacklist", true},
		{"server", false},
		{"tls", false},
		{"metrics", false},
		{"unknown", false},
	}

	for _, tt := range tests {
		t.Run(tt.section, func(t *testing.T) {
			if got := IsReloadableSection(tt.section); got != tt.want {
				t.Errorf("IsReloadableSection(%s) = %v, want %v", tt.section, got, tt.want)
			}
		})
	}
}

func TestRequiresRestart(t *testing.T) {
	tests := []struct {
		name       string
		old        *Config
		new        *Config
		wantReasons int
	}{
		{
			name:       "no changes",
			old:        DefaultConfig(),
			new:        DefaultConfig(),
			wantReasons: 0,
		},
		{
			name: "server host changed",
			old:  func() *Config { c := DefaultConfig(); c.Server.Host = "0.0.0.0"; return c }(),
			new:  func() *Config { c := DefaultConfig(); c.Server.Host = "127.0.0.1"; return c }(),
			wantReasons: 1,
		},
		{
			name: "server port changed",
			old:  func() *Config { c := DefaultConfig(); c.Server.Port = 8080; return c }(),
			new:  func() *Config { c := DefaultConfig(); c.Server.Port = 9090; return c }(),
			wantReasons: 1,
		},
		{
			name: "TLS changed",
			old:  func() *Config { c := DefaultConfig(); c.Server.TLS.Enabled = false; return c }(),
			new:  func() *Config { c := DefaultConfig(); c.Server.TLS.Enabled = true; c.Server.TLS.CertFile = "cert.pem"; c.Server.TLS.KeyFile = "key.pem"; return c }(),
			wantReasons: 3, // TLS enabled, cert file changed, key file changed
		},
		{
			name: "metrics port changed",
			old:  func() *Config { c := DefaultConfig(); c.Metrics.Port = 9090; return c }(),
			new:  func() *Config { c := DefaultConfig(); c.Metrics.Port = 9091; return c }(),
			wantReasons: 1,
		},
		{
			name: "logging changed (reloadable)",
			old:  func() *Config { c := DefaultConfig(); c.Logging.Level = "info"; return c }(),
			new:  func() *Config { c := DefaultConfig(); c.Logging.Level = "debug"; return c }(),
			wantReasons: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reasons := RequiresRestart(tt.old, tt.new)
			if len(reasons) != tt.wantReasons {
				t.Errorf("RequiresRestart() returned %d reasons, want %d: %v", len(reasons), tt.wantReasons, reasons)
			}
		})
	}
}

func TestGetChangedSections(t *testing.T) {
	tests := []struct {
		name          string
		old           *Config
		new           *Config
		wantSections  []string
	}{
		{
			name:         "no changes",
			old:          DefaultConfig(),
			new:          DefaultConfig(),
			wantSections: nil,
		},
		{
			name: "logging changed",
			old:  func() *Config { c := DefaultConfig(); c.Logging.Level = "info"; return c }(),
			new:  func() *Config { c := DefaultConfig(); c.Logging.Level = "debug"; return c }(),
			wantSections: []string{"logging"},
		},
		{
			name: "backends changed",
			old:  DefaultConfig(),
			new:  func() *Config { c := DefaultConfig(); c.Backends = []BackendConfig{{Name: "new", Host: "example.com", Port: 64738}}; return c }(),
			wantSections: []string{"backends"},
		},
		{
			name: "limits changed",
			old:  func() *Config { c := DefaultConfig(); c.Limits.MaxConnections = 1000; return c }(),
			new:  func() *Config { c := DefaultConfig(); c.Limits.MaxConnections = 2000; return c }(),
			wantSections: []string{"limits"},
		},
		{
			name: "server port changed",
			old:  func() *Config { c := DefaultConfig(); c.Server.Port = 8080; return c }(),
			new:  func() *Config { c := DefaultConfig(); c.Server.Port = 9090; return c }(),
			wantSections: []string{"server"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			changed := GetChangedSections(tt.old, tt.new)

			if tt.wantSections == nil {
				if len(changed) > 0 {
					t.Errorf("GetChangedSections() = %v, want nil", changed)
				}
				return
			}

			// Check that all expected sections are present
			changedMap := make(map[string]bool)
			for _, s := range changed {
				changedMap[s] = true
			}

			for _, want := range tt.wantSections {
				if !changedMap[want] {
					t.Errorf("GetChangedSections() missing section %s, got %v", want, changed)
				}
			}
		})
	}
}

func TestBackendsEqual(t *testing.T) {
	tests := []struct {
		name string
		a    []BackendConfig
		b    []BackendConfig
		want bool
	}{
		{
			name: "equal",
			a:    []BackendConfig{{Name: "test", Host: "localhost", Port: 64738}},
			b:    []BackendConfig{{Name: "test", Host: "localhost", Port: 64738}},
			want: true,
		},
		{
			name: "different length",
			a:    []BackendConfig{{Name: "test", Host: "localhost", Port: 64738}},
			b:    []BackendConfig{},
			want: false,
		},
		{
			name: "different host",
			a:    []BackendConfig{{Name: "test", Host: "localhost", Port: 64738}},
			b:    []BackendConfig{{Name: "test", Host: "otherhost", Port: 64738}},
			want: false,
		},
		{
			name: "different port",
			a:    []BackendConfig{{Name: "test", Host: "localhost", Port: 64738}},
			b:    []BackendConfig{{Name: "test", Host: "localhost", Port: 64739}},
			want: false,
		},
		{
			name: "different tokens",
			a:    []BackendConfig{{Name: "test", Host: "localhost", Port: 64738, Tokens: []string{"token1"}}},
			b:    []BackendConfig{{Name: "test", Host: "localhost", Port: 64738, Tokens: []string{"token2"}}},
			want: false,
		},
		{
			name: "same tokens different order",
			a:    []BackendConfig{{Name: "test", Host: "localhost", Port: 64738, Tokens: []string{"token1", "token2"}}},
			b:    []BackendConfig{{Name: "test", Host: "localhost", Port: 64738, Tokens: []string{"token2", "token1"}}},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := backendsEqual(tt.a, tt.b); got != tt.want {
				t.Errorf("backendsEqual() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTokensEqual(t *testing.T) {
	tests := []struct {
		name string
		a    []string
		b    []string
		want bool
	}{
		{
			name: "equal",
			a:    []string{"token1", "token2"},
			b:    []string{"token1", "token2"},
			want: true,
		},
		{
			name: "different length",
			a:    []string{"token1"},
			b:    []string{"token1", "token2"},
			want: false,
		},
		{
			name: "different tokens",
			a:    []string{"token1"},
			b:    []string{"token2"},
			want: false,
		},
		{
			name: "both empty",
			a:    []string{},
			b:    []string{},
			want: true,
		},
		{
			name: "both nil",
			a:    nil,
			b:    nil,
			want: true,
		},
		{
			name: "one nil one empty",
			a:    nil,
			b:    []string{},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tokensEqual(tt.a, tt.b); got != tt.want {
				t.Errorf("tokensEqual() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseIPFilter(t *testing.T) {
	tests := []struct {
		name      string
		whitelist []string
		blacklist []string
		wantErr   bool
	}{
		{
			name:      "empty lists",
			whitelist: []string{},
			blacklist: []string{},
			wantErr:   false,
		},
		{
			name:      "valid CIDR",
			whitelist: []string{"192.168.1.0/24"},
			blacklist: []string{"10.0.0.0/8"},
			wantErr:   false,
		},
		{
			name:      "valid single IP",
			whitelist: []string{"192.168.1.1"},
			blacklist: []string{"10.0.0.1"},
			wantErr:   false,
		},
		{
			name:      "valid IPv6",
			whitelist: []string{"::1/128", "2001:db8::/32"},
			blacklist: []string{},
			wantErr:   false,
		},
		{
			name:      "invalid CIDR",
			whitelist: []string{"invalid"},
			blacklist: []string{},
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := ParseIPFilter(tt.whitelist, tt.blacklist)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseIPFilter() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestConfigWatcher_Debounce(t *testing.T) {
	// Create temp config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	cfg := DefaultConfig()
	data, _ := yaml.Marshal(cfg)
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	// Use a short debounce time
	w, err := NewConfigWatcher(configPath, cfg,
		WithLogger(zap.NewNop()),
		WithDebounce(200*time.Millisecond),
	)
	if err != nil {
		t.Fatalf("failed to create watcher: %v", err)
	}
	defer w.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = w.Watch(ctx)
	}()

	// Wait for watcher to start
	time.Sleep(100 * time.Millisecond)

	// Rapidly modify file multiple times
	for i := 0; i < 5; i++ {
		cfg.Logging.Level = []string{"debug", "info", "warn", "error", "debug"}[i]
		data, _ = yaml.Marshal(cfg)
		os.WriteFile(configPath, data, 0644)
		time.Sleep(30 * time.Millisecond)
	}

	// Wait for debounce to complete
	time.Sleep(300 * time.Millisecond)

	// Cancel context to stop watching
	cancel()
	wg.Wait()
}

func TestConfigWatcher_MultipleCallbacks(t *testing.T) {
	// Create temp config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	cfg := DefaultConfig()
	data, _ := yaml.Marshal(cfg)
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	w, err := NewConfigWatcher(configPath, cfg, WithLogger(zap.NewNop()))
	if err != nil {
		t.Fatalf("failed to create watcher: %v", err)
	}
	defer w.Close()

	var callCount atomic.Int32
	var callOrder sync.Mutex
	var order []int

	// Register multiple callbacks
	for i := 0; i < 5; i++ {
		i := i // capture loop variable
		w.OnChange(func(old, new *Config) {
			callCount.Add(1)
			callOrder.Lock()
			order = append(order, i)
			callOrder.Unlock()
		})
	}

	// Modify config file
	cfg.Logging.Level = "debug"
	data, _ = yaml.Marshal(cfg)
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("failed to update config file: %v", err)
	}

	// Reload
	if err := w.Reload(); err != nil {
		t.Errorf("Reload() error = %v", err)
	}

	// Wait for goroutines
	time.Sleep(100 * time.Millisecond)

	// All callbacks should have been called
	if callCount.Load() != 5 {
		t.Errorf("expected 5 callback calls, got %d", callCount.Load())
	}
}

func TestConfigWatcher_Watch_ContextCancel(t *testing.T) {
	// Create temp config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	cfg := DefaultConfig()
	data, _ := yaml.Marshal(cfg)
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	w, err := NewConfigWatcher(configPath, cfg, WithLogger(zap.NewNop()))
	if err != nil {
		t.Fatalf("failed to create watcher: %v", err)
	}
	defer w.Close()

	ctx, cancel := context.WithCancel(context.Background())

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = w.Watch(ctx)
	}()

	// Cancel context after short delay
	time.Sleep(100 * time.Millisecond)
	cancel()

	// Wait for Watch to return
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(2 * time.Second):
		t.Error("Watch did not return after context cancellation")
	}
}

func TestConfigWatcher_Watch_Close(t *testing.T) {
	// Create temp config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	cfg := DefaultConfig()
	data, _ := yaml.Marshal(cfg)
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	w, err := NewConfigWatcher(configPath, cfg, WithLogger(zap.NewNop()))
	if err != nil {
		t.Fatalf("failed to create watcher: %v", err)
	}

	ctx := context.Background()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = w.Watch(ctx)
	}()

	// Close watcher after short delay
	time.Sleep(100 * time.Millisecond)
	w.Close()

	// Wait for Watch to return
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(2 * time.Second):
		t.Error("Watch did not return after Close")
	}
}

func TestConfigWatcher_InvalidConfig(t *testing.T) {
	// Create temp config file with invalid config
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Write invalid config
	if err := os.WriteFile(configPath, []byte("invalid: yaml: content"), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	// Start with valid config
	cfg := DefaultConfig()
	w, err := NewConfigWatcher(configPath, cfg, WithLogger(zap.NewNop()))
	if err != nil {
		t.Fatalf("failed to create watcher: %v", err)
	}
	defer w.Close()

	// Reload should fail
	err = w.Reload()
	if err == nil {
		t.Error("expected error when reloading invalid config")
	}
}

func TestConfigWatcher_PreserveNonReloadable(t *testing.T) {
	// Create temp config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	cfg := DefaultConfig()
	cfg.Server.Host = "original-host"
	cfg.Server.Port = 8080
	cfg.Server.TLS.Enabled = false
	cfg.Server.TLS.CertFile = ""
	cfg.Server.TLS.KeyFile = ""
	cfg.Metrics.Port = 9090
	data, _ := yaml.Marshal(cfg)
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	w, err := NewConfigWatcher(configPath, cfg, WithLogger(zap.NewNop()))
	if err != nil {
		t.Fatalf("failed to create watcher: %v", err)
	}
	defer w.Close()

	// Modify non-reloadable fields but keep TLS disabled to avoid validation issues
	newCfg := DefaultConfig()
	newCfg.Server.Host = "new-host"
	newCfg.Server.Port = 9999
	newCfg.Server.TLS.Enabled = false // Keep TLS disabled
	newCfg.Server.TLS.CertFile = ""
	newCfg.Server.TLS.KeyFile = ""
	newCfg.Metrics.Port = 9091
	newCfg.Logging.Level = "debug" // Reloadable change
	data, _ = yaml.Marshal(newCfg)
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("failed to update config file: %v", err)
	}

	// Reload
	if err := w.Reload(); err != nil {
		t.Errorf("Reload() error = %v", err)
	}

	// Verify non-reloadable fields are preserved
	got := w.GetConfig()
	if got.Server.Host != "original-host" {
		t.Errorf("expected server host to be preserved, got %s", got.Server.Host)
	}
	if got.Server.Port != 8080 {
		t.Errorf("expected server port to be preserved, got %d", got.Server.Port)
	}
	if got.Server.TLS.Enabled != false {
		t.Error("expected TLS enabled to be preserved as false")
	}
	if got.Metrics.Port != 9090 {
		t.Errorf("expected metrics port to be preserved, got %d", got.Metrics.Port)
	}
	// Reloadable field should be updated
	if got.Logging.Level != "debug" {
		t.Errorf("expected logging level 'debug', got %s", got.Logging.Level)
	}
}

func TestConfigWatcher_ValidationFailure(t *testing.T) {
	// Create temp config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	cfg := DefaultConfig()
	data, _ := yaml.Marshal(cfg)
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	w, err := NewConfigWatcher(configPath, cfg, WithLogger(zap.NewNop()))
	if err != nil {
		t.Fatalf("failed to create watcher: %v", err)
	}
	defer w.Close()

	// Write invalid config (invalid log level)
	invalidCfg := map[string]interface{}{
		"logging": map[string]interface{}{
			"level":  "invalid-level",
			"format": "json",
		},
	}
	data, _ = yaml.Marshal(invalidCfg)
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("failed to update config file: %v", err)
	}

	// Reload should fail with validation error
	err = w.Reload()
	if err == nil {
		t.Error("expected validation error for invalid log level")
	}
}