package config

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"go.uber.org/zap"
)

// Reloader 配置重载器
type Reloader struct {
	configPath string
	config     *Config
	logger     *zap.Logger
	mu         sync.RWMutex
	handlers   []ConfigChangeHandler
	stopCh     chan struct{}
}

// ConfigChangeHandler 配置变更处理器
type ConfigChangeHandler func(oldConfig, newConfig *Config) error

// NewReloader 创建配置重载器
func NewReloader(configPath string, initialConfig *Config, logger *zap.Logger) *Reloader {
	return &Reloader{
		configPath: configPath,
		config:     initialConfig,
		logger:     logger,
		handlers:   make([]ConfigChangeHandler, 0),
		stopCh:     make(chan struct{}),
	}
}

// OnConfigChange 注册配置变更处理器
func (r *Reloader) OnConfigChange(handler ConfigChangeHandler) {
	r.handlers = append(r.handlers, handler)
}

// GetConfig 获取当前配置
func (r *Reloader) GetConfig() *Config {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.config
}

// Reload 重新加载配置
func (r *Reloader) Reload() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.logger.Info("reloading configuration",
		zap.String("path", r.configPath),
	)

	newConfig, err := Load(r.configPath)
	if err != nil {
		r.logger.Error("failed to reload config",
			zap.Error(err),
		)
		return fmt.Errorf("failed to reload config: %w", err)
	}

	// 验证新配置
	if err := newConfig.Validate(); err != nil {
		r.logger.Error("invalid config after reload",
			zap.Error(err),
		)
		return fmt.Errorf("invalid config: %w", err)
	}

	oldConfig := r.config
	r.config = newConfig

	// 调用所有处理器
	for i, handler := range r.handlers {
		if err := handler(oldConfig, newConfig); err != nil {
			r.logger.Error("config change handler failed",
				zap.Int("handler_index", i),
				zap.Error(err),
			)
			// 回滚
			r.config = oldConfig
			return fmt.Errorf("handler %d failed: %w", i, err)
		}
	}

	r.logger.Info("configuration reloaded successfully",
		zap.String("murmur_host", newConfig.Murmur.Host),
		zap.Int("murmur_port", newConfig.Murmur.Port),
		zap.Int("backends", len(newConfig.Backends)),
	)

	return nil
}

// StartSignalWatcher 启动信号监听器
func (r *Reloader) StartSignalWatcher(ctx context.Context) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGHUP)

	go func() {
		for {
			select {
			case sig := <-sigCh:
				if sig == syscall.SIGHUP {
					r.logger.Info("received SIGHUP signal, reloading config")
					if err := r.Reload(); err != nil {
						r.logger.Error("config reload failed", zap.Error(err))
					}
				}
			case <-ctx.Done():
				return
			case <-r.stopCh:
				return
			}
		}
	}()
}

// Stop 停止重载器
func (r *Reloader) Stop() {
	close(r.stopCh)
}

// Watcher 文件监听器接口
type Watcher interface {
	Start() error
	Stop()
	Events() <-chan WatchEvent
}

// WatchEvent 文件监听事件
type WatchEvent struct {
	Path string
	Op   WatchOp
}

// WatchOp 监听操作类型
type WatchOp int

const (
	WatchOpCreate WatchOp = iota
	WatchOpWrite
	WatchOpRemove
	WatchOpRename
)

// FileWatcher 文件监听器
type FileWatcher struct {
	path   string
	events chan WatchEvent
	stopCh chan struct{}
}

// NewFileWatcher 创建文件监听器
func NewFileWatcher(path string) *FileWatcher {
	return &FileWatcher{
		path:   path,
		events: make(chan WatchEvent, 10),
		stopCh: make(chan struct{}),
	}
}

// Start 启动监听
func (w *FileWatcher) Start() error {
	// 简化实现：使用轮询检查文件修改时间
	go w.pollLoop()
	return nil
}

// Stop 停止监听
func (w *FileWatcher) Stop() {
	close(w.stopCh)
}

// Events 返回事件通道
func (w *FileWatcher) Events() <-chan WatchEvent {
	return w.events
}

func (w *FileWatcher) pollLoop() {
	var lastMod int64

	for {
		select {
		case <-w.stopCh:
			return
		default:
		}

		info, err := os.Stat(w.path)
		if err == nil {
			modTime := info.ModTime().Unix()
			if lastMod > 0 && modTime > lastMod {
				select {
				case w.events <- WatchEvent{Path: w.path, Op: WatchOpWrite}:
				default:
					// 事件队列满，跳过
				}
			}
			lastMod = modTime
		}

		// 使用系统调用等待
		select {
		case <-w.stopCh:
			return
		}
	}
}

// ConfigDiff 配置差异
type ConfigDiff struct {
	MurmurHostChanged  bool
	MurmurPortChanged  bool
	BackendsAdded      []string
	BackendsRemoved    []string
	BackendsModified   []string
	TokensChanged      bool
	LimitsChanged      bool
	MetricsChanged     bool
	LoggingChanged     bool
}

// DiffConfigs 比较两个配置的差异
func DiffConfigs(old, new *Config) *ConfigDiff {
	diff := &ConfigDiff{}

	// 比较 Murmur 配置
	if old.Murmur.Host != new.Murmur.Host {
		diff.MurmurHostChanged = true
	}
	if old.Murmur.Port != new.Murmur.Port {
		diff.MurmurPortChanged = true
	}

	// 比较后端配置
	oldBackends := make(map[string]BackendConfig)
	for _, b := range old.Backends {
		oldBackends[b.Name] = b
	}

	for _, b := range new.Backends {
		if oldB, exists := oldBackends[b.Name]; exists {
			// 检查是否修改
			if oldB.Host != b.Host || oldB.Port != b.Port {
				diff.BackendsModified = append(diff.BackendsModified, b.Name)
			}
			// 检查 Token 变化
			if !equalSlices(oldB.Tokens, b.Tokens) {
				diff.TokensChanged = true
			}
			delete(oldBackends, b.Name)
		} else {
			diff.BackendsAdded = append(diff.BackendsAdded, b.Name)
		}
	}

	// 剩余的是被删除的
	for name := range oldBackends {
		diff.BackendsRemoved = append(diff.BackendsRemoved, name)
	}

	// 比较限制配置
	if old.Limits.MaxConnections != new.Limits.MaxConnections ||
		old.Limits.MaxConnectionsPerIP != new.Limits.MaxConnectionsPerIP {
		diff.LimitsChanged = true
	}

	// 比较指标配置
	if old.Metrics.Enabled != new.Metrics.Enabled ||
		old.Metrics.Port != new.Metrics.Port {
		diff.MetricsChanged = true
	}

	// 比较日志配置
	if old.Logging.Level != new.Logging.Level ||
		old.Logging.Format != new.Logging.Format {
		diff.LoggingChanged = true
	}

	return diff
}

func equalSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}