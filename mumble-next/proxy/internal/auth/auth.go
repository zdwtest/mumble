package auth

import (
	"sync"

	"github.com/mumble/mumble-next/proxy/internal/config"
)

// TokenManager Token 管理器
type TokenManager struct {
	backendTokens map[string][]string // token -> backend name
	backends      map[string]*Backend // backend name -> backend
	defaultBackend *Backend
	mu            sync.RWMutex
}

// Backend 后端服务器
type Backend struct {
	Name string
	Host string
	Port int
}

// NewTokenManager 创建 Token 管理器
func NewTokenManager() *TokenManager {
	return &TokenManager{
		backendTokens: make(map[string][]string),
		backends:      make(map[string]*Backend),
	}
}

// LoadFromConfig 从配置加载
func (tm *TokenManager) LoadFromConfig(cfg *config.Config) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	// 清空现有配置
	tm.backendTokens = make(map[string][]string)
	tm.backends = make(map[string]*Backend)

	// 设置默认后端
	defaultBackend := cfg.GetDefaultBackend()
	tm.defaultBackend = &Backend{
		Name: defaultBackend.Name,
		Host: defaultBackend.Host,
		Port: defaultBackend.Port,
	}
	tm.backends[defaultBackend.Name] = tm.defaultBackend

	// 加载多租户后端
	for _, bc := range cfg.Backends {
		backend := &Backend{
			Name: bc.Name,
			Host: bc.Host,
			Port: bc.Port,
		}
		tm.backends[bc.Name] = backend

		// 建立 token 到后端的映射
		for _, token := range bc.Tokens {
			tm.backendTokens[token] = append(tm.backendTokens[token], bc.Name)
		}
	}
}

// Validate 验证 Token 并返回对应后端
func (tm *TokenManager) Validate(token string) (*Backend, bool) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	// 如果没有配置 token，返回默认后端
	if len(tm.backendTokens) == 0 {
		return tm.defaultBackend, true
	}

	// 空 token 返回默认后端（如果认证未启用）
	if token == "" {
		return nil, false
	}

	// 查找 token 对应的后端
	backendNames, exists := tm.backendTokens[token]
	if !exists || len(backendNames) == 0 {
		return nil, false
	}

	backend, exists := tm.backends[backendNames[0]]
	if !exists {
		return nil, false
	}

	return backend, true
}

// GetBackend 获取指定后端
func (tm *TokenManager) GetBackend(name string) (*Backend, bool) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	backend, exists := tm.backends[name]
	return backend, exists
}

// GetDefaultBackend 获取默认后端
func (tm *TokenManager) GetDefaultBackend() *Backend {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	return tm.defaultBackend
}

// AddBackend 添加后端
func (tm *TokenManager) AddBackend(name, host string, port int, tokens []string) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	backend := &Backend{
		Name: name,
		Host: host,
		Port: port,
	}
	tm.backends[name] = backend

	for _, token := range tokens {
		tm.backendTokens[token] = append(tm.backendTokens[token], name)
	}
}

// RemoveBackend 移除后端
func (tm *TokenManager) RemoveBackend(name string) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	delete(tm.backends, name)

	// 移除相关的 token 映射
	for token, backends := range tm.backendTokens {
		for i, b := range backends {
			if b == name {
				tm.backendTokens[token] = append(backends[:i], backends[i+1:]...)
				break
			}
		}
		if len(tm.backendTokens[token]) == 0 {
			delete(tm.backendTokens, token)
		}
	}
}

// ListBackends 列出所有后端
func (tm *TokenManager) ListBackends() []*Backend {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	backends := make([]*Backend, 0, len(tm.backends))
	for _, b := range tm.backends {
		backends = append(backends, b)
	}
	return backends
}