package auth

import (
	"testing"

	"github.com/mumble/mumble-next/proxy/internal/config"
)

func TestNewTokenManager(t *testing.T) {
	tm := NewTokenManager()
	if tm == nil {
		t.Fatal("expected token manager to be created")
	}
}

func TestTokenManager_Validate_NoTokens(t *testing.T) {
	tm := NewTokenManager()
	cfg := config.DefaultConfig()
	tm.LoadFromConfig(cfg)

	// 当没有配置 tokens 时，应该返回默认后端
	backend, ok := tm.Validate("any-token")
	if !ok {
		t.Error("expected validation to succeed when no tokens configured")
	}

	if backend.Name != "default" {
		t.Errorf("expected default backend, got %s", backend.Name)
	}
}

func TestTokenManager_Validate_ValidToken(t *testing.T) {
	tm := NewTokenManager()
	cfg := config.DefaultConfig()
	cfg.Backends = []config.BackendConfig{
		{
			Name:   "server1",
			Host:   "server1.example.com",
			Port:   64738,
			Tokens: []string{"token1", "token2"},
		},
	}
	tm.LoadFromConfig(cfg)

	backend, ok := tm.Validate("token1")
	if !ok {
		t.Fatal("expected validation to succeed")
	}

	if backend.Name != "server1" {
		t.Errorf("expected backend 'server1', got %s", backend.Name)
	}

	if backend.Host != "server1.example.com" {
		t.Errorf("expected host 'server1.example.com', got %s", backend.Host)
	}
}

func TestTokenManager_Validate_InvalidToken(t *testing.T) {
	tm := NewTokenManager()
	cfg := config.DefaultConfig()
	cfg.Backends = []config.BackendConfig{
		{
			Name:   "server1",
			Host:   "server1.example.com",
			Port:   64738,
			Tokens: []string{"token1"},
		},
	}
	tm.LoadFromConfig(cfg)

	_, ok := tm.Validate("invalid-token")
	if ok {
		t.Error("expected validation to fail for invalid token")
	}
}

func TestTokenManager_Validate_EmptyToken(t *testing.T) {
	tm := NewTokenManager()
	cfg := config.DefaultConfig()
	cfg.Backends = []config.BackendConfig{
		{
			Name:   "server1",
			Host:   "server1.example.com",
			Port:   64738,
			Tokens: []string{"token1"},
		},
	}
	tm.LoadFromConfig(cfg)

	_, ok := tm.Validate("")
	if ok {
		t.Error("expected validation to fail for empty token when tokens are configured")
	}
}

func TestTokenManager_AddBackend(t *testing.T) {
	tm := NewTokenManager()
	cfg := config.DefaultConfig()
	tm.LoadFromConfig(cfg)

	tm.AddBackend("new-server", "new.example.com", 64738, []string{"new-token"})

	// 验证后端已添加
	backend, ok := tm.GetBackend("new-server")
	if !ok {
		t.Fatal("expected backend to exist")
	}

	if backend.Host != "new.example.com" {
		t.Errorf("expected host 'new.example.com', got %s", backend.Host)
	}

	// 验证 token 已映射
	validatedBackend, ok := tm.Validate("new-token")
	if !ok {
		t.Fatal("expected token to be valid")
	}

	if validatedBackend.Name != "new-server" {
		t.Errorf("expected backend 'new-server', got %s", validatedBackend.Name)
	}
}

func TestTokenManager_RemoveBackend(t *testing.T) {
	tm := NewTokenManager()
	cfg := config.DefaultConfig()
	cfg.Backends = []config.BackendConfig{
		{
			Name:   "server1",
			Host:   "server1.example.com",
			Port:   64738,
			Tokens: []string{"token1"},
		},
	}
	tm.LoadFromConfig(cfg)

	// 验证存在
	_, ok := tm.GetBackend("server1")
	if !ok {
		t.Fatal("expected backend to exist")
	}

	tm.RemoveBackend("server1")

	// 验证已移除
	_, ok = tm.GetBackend("server1")
	if ok {
		t.Error("expected backend to be removed")
	}

	// 验证 token 不再映射到已删除的后端
	backend, _ := tm.Validate("token1")
	if backend != nil && backend.Name == "server1" {
		t.Error("expected token not to map to removed backend")
	}
}

func TestTokenManager_GetDefaultBackend(t *testing.T) {
	tm := NewTokenManager()
	cfg := config.DefaultConfig()
	cfg.Murmur.Host = "mumble.example.com"
	cfg.Murmur.Port = 64738
	tm.LoadFromConfig(cfg)

	backend := tm.GetDefaultBackend()
	if backend == nil {
		t.Fatal("expected default backend to exist")
	}

	if backend.Host != "mumble.example.com" {
		t.Errorf("expected host 'mumble.example.com', got %s", backend.Host)
	}
}

func TestTokenManager_ListBackends(t *testing.T) {
	tm := NewTokenManager()
	cfg := config.DefaultConfig()
	cfg.Backends = []config.BackendConfig{
		{
			Name:   "server1",
			Host:   "server1.example.com",
			Port:   64738,
			Tokens: []string{"token1"},
		},
		{
			Name:   "server2",
			Host:   "server2.example.com",
			Port:   64738,
			Tokens: []string{"token2"},
		},
	}
	tm.LoadFromConfig(cfg)

	backends := tm.ListBackends()

	// 应该有 3 个后端：default + 2 个配置的
	if len(backends) != 3 {
		t.Errorf("expected 3 backends, got %d", len(backends))
	}
}

func TestTokenManager_Reload(t *testing.T) {
	tm := NewTokenManager()

	// 第一次加载
	cfg1 := config.DefaultConfig()
	cfg1.Backends = []config.BackendConfig{
		{
			Name:   "server1",
			Host:   "server1.example.com",
			Port:   64738,
			Tokens: []string{"token1"},
		},
	}
	tm.LoadFromConfig(cfg1)

	// 验证第一次加载
	_, ok := tm.Validate("token1")
	if !ok {
		t.Fatal("expected token1 to be valid")
	}

	// 第二次加载（重载）
	cfg2 := config.DefaultConfig()
	cfg2.Backends = []config.BackendConfig{
		{
			Name:   "server2",
			Host:   "server2.example.com",
			Port:   64738,
			Tokens: []string{"token2"},
		},
	}
	tm.LoadFromConfig(cfg2)

	// 验证旧 token 已失效
	_, ok = tm.Validate("token1")
	if ok {
		t.Error("expected token1 to be invalid after reload")
	}

	// 验证新 token 有效
	_, ok = tm.Validate("token2")
	if !ok {
		t.Error("expected token2 to be valid after reload")
	}
}