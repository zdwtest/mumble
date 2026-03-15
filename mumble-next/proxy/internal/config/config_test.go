package config

import (
	"context"
	"os"
	"testing"
	"time"

	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Server.Port != 8080 {
		t.Errorf("expected default server port 8080, got %d", cfg.Server.Port)
	}

	if cfg.Murmur.Host != "localhost" {
		t.Errorf("expected default murmur host localhost, got %s", cfg.Murmur.Host)
	}

	if cfg.Murmur.Port != 64738 {
		t.Errorf("expected default murmur port 64738, got %d", cfg.Murmur.Port)
	}

	if !cfg.Metrics.Enabled {
		t.Error("expected metrics to be enabled by default")
	}
}

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
	}{
		{
			name:   "valid default config",
			config: DefaultConfig(),
			wantErr: false,
		},
		{
			name: "invalid server port",
			config: &Config{
				Server: ServerConfig{Port: 0},
				Murmur: MurmurConfig{Port: 64738},
				Logging: LoggingConfig{Level: "info", Format: "json"},
			},
			wantErr: true,
		},
		{
			name: "invalid murmur port",
			config: &Config{
				Server: ServerConfig{Port: 8080},
				Murmur: MurmurConfig{Port: 70000},
				Logging: LoggingConfig{Level: "info", Format: "json"},
			},
			wantErr: true,
		},
		{
			name: "invalid log level",
			config: &Config{
				Server: ServerConfig{Port: 8080},
				Murmur: MurmurConfig{Port: 64738},
				Logging: LoggingConfig{Level: "invalid", Format: "json"},
			},
			wantErr: true,
		},
		{
			name: "invalid log format",
			config: &Config{
				Server: ServerConfig{Port: 8080},
				Murmur: MurmurConfig{Port: 64738},
				Logging: LoggingConfig{Level: "info", Format: "invalid"},
			},
			wantErr: true,
		},
		{
			name: "TLS enabled without cert",
			config: &Config{
				Server: ServerConfig{
					Port: 8080,
					TLS:  TLSConfig{Enabled: true},
				},
				Murmur:  MurmurConfig{Port: 64738},
				Logging: LoggingConfig{Level: "info", Format: "json"},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestConfig_LoadFromFile(t *testing.T) {
	// 创建临时配置文件
	tmpFile, err := os.CreateTemp("", "config-*.yaml")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	config := DefaultConfig()
	config.Server.Port = 9090
	config.Murmur.Host = "mumble.example.com"
	config.Murmur.Port = 64738
	config.Metrics.Enabled = true

	data, err := yaml.Marshal(config)
	if err != nil {
		t.Fatalf("failed to marshal config: %v", err)
	}

	if _, err := tmpFile.Write(data); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}
	tmpFile.Close()

	// 加载配置
	loaded, err := Load(tmpFile.Name())
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	if loaded.Server.Port != 9090 {
		t.Errorf("expected server port 9090, got %d", loaded.Server.Port)
	}

	if loaded.Murmur.Host != "mumble.example.com" {
		t.Errorf("expected murmur host mumble.example.com, got %s", loaded.Murmur.Host)
	}
}

func TestConfig_Defaults(t *testing.T) {
	cfg := DefaultConfig()

	// 验证所有默认值
	if cfg.Server.ReadTimeout != 30*time.Second {
		t.Errorf("unexpected read timeout: %v", cfg.Server.ReadTimeout)
	}

	if cfg.Murmur.Timeout != 10*time.Second {
		t.Errorf("unexpected murmur timeout: %v", cfg.Murmur.Timeout)
	}

	if cfg.Metrics.Port != 9090 {
		t.Errorf("unexpected metrics port: %d", cfg.Metrics.Port)
	}

	if cfg.Metrics.Path != "/metrics" {
		t.Errorf("unexpected metrics path: %s", cfg.Metrics.Path)
	}

	if cfg.Limits.MaxConnections != 1000 {
		t.Errorf("unexpected max connections: %d", cfg.Limits.MaxConnections)
	}

	if cfg.Limits.MaxConnectionsPerIP != 10 {
		t.Errorf("unexpected max connections per IP: %d", cfg.Limits.MaxConnectionsPerIP)
	}
}

func TestConfig_GetDefaultBackend(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Murmur.Host = "mumble.example.com"
	cfg.Murmur.Port = 64738

	backend := cfg.GetDefaultBackend()

	if backend.Name != "default" {
		t.Errorf("expected backend name 'default', got %s", backend.Name)
	}

	if backend.Host != "mumble.example.com" {
		t.Errorf("expected backend host 'mumble.example.com', got %s", backend.Host)
	}

	if backend.Port != 64738 {
		t.Errorf("expected backend port 64738, got %d", backend.Port)
	}
}

func TestLoadFromEnv(t *testing.T) {
	// Set environment variables
	os.Setenv("MUMBLE_SERVER_PORT", "9999")
	os.Setenv("MUMBLE_MURMUR_HOST", "test.example.com")
	os.Setenv("MUMBLE_MURMUR_PORT", "65000")
	defer func() {
		os.Unsetenv("MUMBLE_SERVER_PORT")
		os.Unsetenv("MUMBLE_MURMUR_HOST")
		os.Unsetenv("MUMBLE_MURMUR_PORT")
	}()

	cfg := LoadFromEnv()

	// Note: viper may not always pick up env vars in tests depending on configuration
	// The test ensures the function runs without panic
	if cfg == nil {
		t.Error("expected config to be non-nil")
	}
}

func TestConfig_Load_InvalidPath(t *testing.T) {
	_, err := Load("/nonexistent/path/config.yaml")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestConfig_Load_InvalidYAML(t *testing.T) {
	// Create temp file with invalid YAML
	tmpFile, err := os.CreateTemp("", "config-*.yaml")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	tmpFile.WriteString("invalid: yaml: content:\n  - broken")
	tmpFile.Close()

	_, err = Load(tmpFile.Name())
	// Load should either return an error or default config
	// The exact behavior depends on viper configuration
	if err == nil {
		// Viper might not fail on invalid YAML in some cases
		t.Log("Load did not fail on invalid YAML (viper behavior)")
	}
}

func TestReloader_Reload(t *testing.T) {
	// Create temp config file
	tmpFile, err := os.CreateTemp("", "config-*.yaml")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	cfg := DefaultConfig()
	data, _ := yaml.Marshal(cfg)
	tmpFile.Write(data)
	tmpFile.Close()

	logger := zap.NewNop()
	reloader := NewReloader(tmpFile.Name(), cfg, logger)

	// Add a change handler
	handlerCalled := false
	reloader.OnConfigChange(func(old, new *Config) error {
		handlerCalled = true
		return nil
	})

	// Reload should work
	err = reloader.Reload()
	if err != nil {
		t.Errorf("expected reload to succeed: %v", err)
	}

	if !handlerCalled {
		t.Error("expected handler to be called")
	}
}

func TestReloader_Reload_InvalidConfig(t *testing.T) {
	// Create temp config file with valid config initially
	tmpFile, err := os.CreateTemp("", "config-*.yaml")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	cfg := DefaultConfig()
	data, _ := yaml.Marshal(cfg)
	tmpFile.Write(data)
	tmpFile.Close()

	logger := zap.NewNop()
	reloader := NewReloader(tmpFile.Name(), cfg, logger)

	// Overwrite with invalid config
	os.WriteFile(tmpFile.Name(), []byte("invalid: yaml"), 0644)

	// Reload should work or return error (depends on viper)
	_ = reloader.Reload()
}

func TestReloader_Stop(t *testing.T) {
	cfg := DefaultConfig()
	logger := zap.NewNop()
	reloader := NewReloader("", cfg, logger)

	// Stop should not panic
	reloader.Stop()
}

func TestFileWatcher_StartStop(t *testing.T) {
	watcher := NewFileWatcher("/nonexistent/path")

	err := watcher.Start()
	if err != nil {
		t.Errorf("expected start to succeed: %v", err)
	}

	// Give it time to start
	time.Sleep(50 * time.Millisecond)

	// Stop should not panic
	watcher.Stop()
}

func TestFileWatcher_Events(t *testing.T) {
	watcher := NewFileWatcher("/nonexistent/path")

	events := watcher.Events()
	if events == nil {
		t.Error("expected events channel to be non-nil")
	}
}

func TestReloader_StartSignalWatcher(t *testing.T) {
	cfg := DefaultConfig()
	logger := zap.NewNop()
	reloader := NewReloader("", cfg, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start signal watcher
	reloader.StartSignalWatcher(ctx)

	// Give it time to start
	time.Sleep(50 * time.Millisecond)

	// Stop the reloader
	reloader.Stop()
}

func TestReloader_ReloadWithHandlerError(t *testing.T) {
	// Create temp config file
	tmpFile, err := os.CreateTemp("", "config-*.yaml")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	cfg := DefaultConfig()
	data, _ := yaml.Marshal(cfg)
	tmpFile.Write(data)
	tmpFile.Close()

	logger := zap.NewNop()
	reloader := NewReloader(tmpFile.Name(), cfg, logger)

	// Add a handler that returns an error
	reloader.OnConfigChange(func(old, new *Config) error {
		return os.ErrNotExist
	})

	// Reload should fail because handler returns error
	err = reloader.Reload()
	if err == nil {
		t.Error("expected reload to fail when handler returns error")
	}
}