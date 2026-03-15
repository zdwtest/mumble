package config

import (
	"testing"

	"go.uber.org/zap"
)

func TestNewReloader(t *testing.T) {
	cfg := DefaultConfig()
	logger := zap.NewNop()

	reloader := NewReloader("", cfg, logger)

	if reloader == nil {
		t.Fatal("expected reloader to be created")
	}

	if reloader.GetConfig() != cfg {
		t.Error("expected config to be set")
	}
}

func TestReloader_GetConfig(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Murmur.Host = "test.example.com"
	logger := zap.NewNop()

	reloader := NewReloader("", cfg, logger)

	got := reloader.GetConfig()
	if got.Murmur.Host != "test.example.com" {
		t.Errorf("expected murmur host 'test.example.com', got %s", got.Murmur.Host)
	}
}

func TestReloader_OnConfigChange(t *testing.T) {
	cfg := DefaultConfig()
	logger := zap.NewNop()

	reloader := NewReloader("", cfg, logger)

	called := false
	reloader.OnConfigChange(func(old, new *Config) error {
		called = true
		return nil
	})

	// 验证处理器已注册
	if len(reloader.handlers) != 1 {
		t.Errorf("expected 1 handler, got %d", len(reloader.handlers))
	}

	_ = called // 在更完整的测试中使用
}

func TestDiffConfigs_NoChange(t *testing.T) {
	old := DefaultConfig()
	new := DefaultConfig()

	diff := DiffConfigs(old, new)

	if diff.MurmurHostChanged {
		t.Error("expected no murmur host change")
	}
	if diff.MurmurPortChanged {
		t.Error("expected no murmur port change")
	}
	if len(diff.BackendsAdded) > 0 {
		t.Errorf("expected no backends added, got %d", len(diff.BackendsAdded))
	}
	if len(diff.BackendsRemoved) > 0 {
		t.Errorf("expected no backends removed, got %d", len(diff.BackendsRemoved))
	}
}

func TestDiffConfigs_MurmurChanged(t *testing.T) {
	old := DefaultConfig()
	old.Murmur.Host = "old.example.com"
	old.Murmur.Port = 64738

	new := DefaultConfig()
	new.Murmur.Host = "new.example.com"
	new.Murmur.Port = 64739

	diff := DiffConfigs(old, new)

	if !diff.MurmurHostChanged {
		t.Error("expected murmur host change")
	}
	if !diff.MurmurPortChanged {
		t.Error("expected murmur port change")
	}
}

func TestDiffConfigs_BackendsAdded(t *testing.T) {
	old := DefaultConfig()

	new := DefaultConfig()
	new.Backends = []BackendConfig{
		{Name: "new-backend", Host: "new.example.com", Port: 64738},
	}

	diff := DiffConfigs(old, new)

	if len(diff.BackendsAdded) != 1 {
		t.Errorf("expected 1 backend added, got %d", len(diff.BackendsAdded))
	}
	if diff.BackendsAdded[0] != "new-backend" {
		t.Errorf("expected 'new-backend', got %s", diff.BackendsAdded[0])
	}
}

func TestDiffConfigs_BackendsRemoved(t *testing.T) {
	old := DefaultConfig()
	old.Backends = []BackendConfig{
		{Name: "old-backend", Host: "old.example.com", Port: 64738},
	}

	new := DefaultConfig()

	diff := DiffConfigs(old, new)

	if len(diff.BackendsRemoved) != 1 {
		t.Errorf("expected 1 backend removed, got %d", len(diff.BackendsRemoved))
	}
	if diff.BackendsRemoved[0] != "old-backend" {
		t.Errorf("expected 'old-backend', got %s", diff.BackendsRemoved[0])
	}
}

func TestDiffConfigs_BackendsModified(t *testing.T) {
	old := DefaultConfig()
	old.Backends = []BackendConfig{
		{Name: "backend1", Host: "old.example.com", Port: 64738},
	}

	new := DefaultConfig()
	new.Backends = []BackendConfig{
		{Name: "backend1", Host: "new.example.com", Port: 64738},
	}

	diff := DiffConfigs(old, new)

	if len(diff.BackendsModified) != 1 {
		t.Errorf("expected 1 backend modified, got %d", len(diff.BackendsModified))
	}
}

func TestDiffConfigs_LimitsChanged(t *testing.T) {
	old := DefaultConfig()
	old.Limits.MaxConnections = 100

	new := DefaultConfig()
	new.Limits.MaxConnections = 200

	diff := DiffConfigs(old, new)

	if !diff.LimitsChanged {
		t.Error("expected limits change")
	}
}

func TestDiffConfigs_MetricsChanged(t *testing.T) {
	old := DefaultConfig()
	old.Metrics.Enabled = false

	new := DefaultConfig()
	new.Metrics.Enabled = true

	diff := DiffConfigs(old, new)

	if !diff.MetricsChanged {
		t.Error("expected metrics change")
	}
}

func TestDiffConfigs_LoggingChanged(t *testing.T) {
	old := DefaultConfig()
	old.Logging.Level = "info"

	new := DefaultConfig()
	new.Logging.Level = "debug"

	diff := DiffConfigs(old, new)

	if !diff.LoggingChanged {
		t.Error("expected logging change")
	}
}

func TestEqualSlices(t *testing.T) {
	tests := []struct {
		a, b   []string
		expect bool
	}{
		{[]string{"a", "b"}, []string{"a", "b"}, true},
		{[]string{"a", "b"}, []string{"a", "c"}, false},
		{[]string{"a"}, []string{"a", "b"}, false},
		{[]string{}, []string{}, true},
		{nil, nil, true},
	}

	for i, tt := range tests {
		result := equalSlices(tt.a, tt.b)
		if result != tt.expect {
			t.Errorf("test %d: expected %v, got %v", i, tt.expect, result)
		}
	}
}