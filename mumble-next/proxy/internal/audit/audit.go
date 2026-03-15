package audit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// EventType 审计事件类型
type EventType string

const (
	// 连接事件
	EventConnect    EventType = "connect"
	EventDisconnect EventType = "disconnect"

	// 认证事件
	EventAuthSuccess EventType = "auth_success"
	EventAuthFailure EventType = "auth_failure"

	// 管理事件
	EventBackendCreate EventType = "backend_create"
	EventBackendDelete EventType = "backend_delete"
	EventBackendUpdate EventType = "backend_update"
	EventConfigReload  EventType = "config_reload"
	EventDrainStart    EventType = "drain_start"
	EventDrainComplete EventType = "drain_complete"
	EventShutdown      EventType = "shutdown"

	// 安全事件
	EventIPBlocked      EventType = "ip_blocked"
	EventRateLimited    EventType = "rate_limited"
	EventTokenInvalid   EventType = "token_invalid"
	EventTokenExpired   EventType = "token_expired"
	EventWhitelistHit   EventType = "whitelist_hit"
	EventBlacklistHit   EventType = "blacklist_hit"

	// 错误事件
	EventError     EventType = "error"
	EventPanic     EventType = "panic"
	EventTimeout   EventType = "timeout"
)

// Event 审计事件
type Event struct {
	// Timestamp 事件时间戳
	Timestamp time.Time `json:"timestamp"`

	// Type 事件类型
	Type EventType `json:"type"`

	// ClientIP 客户端 IP
	ClientIP string `json:"client_ip,omitempty"`

	// Backend 后端名称
	Backend string `json:"backend,omitempty"`

	// UserID 用户标识
	UserID string `json:"user_id,omitempty"`

	// Action 操作描述
	Action string `json:"action"`

	// Resource 资源标识
	Resource string `json:"resource,omitempty"`

	// Result 操作结果
	Result string `json:"result"` // success, failure, denied

	// Details 详细信息
	Details map[string]interface{} `json:"details,omitempty"`

	// Error 错误信息
	Error string `json:"error,omitempty"`

	// Duration 操作耗时
	Duration time.Duration `json:"duration,omitempty"`

	// RequestID 请求 ID
	RequestID string `json:"request_id,omitempty"`
}

// Logger 审计日志记录器
type Logger struct {
	file     *os.File
	filePath string
	enabled  bool
	format   string // "json" or "text"
	mu       sync.Mutex

	// 缓冲
	buffer    []Event
	bufSize   int
	flushInt  time.Duration
	flushChan chan struct{}
	stopChan  chan struct{}

	// 统计
	eventCount int64
	errorCount int64
}

// Config 审计日志配置
type Config struct {
	// Enabled 是否启用
	Enabled bool

	// FilePath 日志文件路径
	FilePath string

	// Format 日志格式 (json, text)
	Format string

	// BufferSize 缓冲大小
	BufferSize int

	// FlushInterval 刷新间隔
	FlushInterval time.Duration
}

// DefaultConfig 默认配置
func DefaultConfig() Config {
	return Config{
		Enabled:       true,
		FilePath:      "logs/audit.log",
		Format:        "json",
		BufferSize:    100,
		FlushInterval: 5 * time.Second,
	}
}

// NewLogger 创建审计日志记录器
func NewLogger(cfg Config) (*Logger, error) {
	logger := &Logger{
		filePath:  cfg.FilePath,
		enabled:   cfg.Enabled,
		format:    cfg.Format,
		bufSize:   cfg.BufferSize,
		flushInt:  cfg.FlushInterval,
		buffer:    make([]Event, 0, cfg.BufferSize),
		flushChan: make(chan struct{}, 1),
		stopChan:  make(chan struct{}),
	}

	if !cfg.Enabled {
		return logger, nil
	}

	// 创建日志目录
	dir := filepath.Dir(cfg.FilePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create audit log directory: %w", err)
	}

	// 打开日志文件
	file, err := os.OpenFile(cfg.FilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open audit log file: %w", err)
	}
	logger.file = file

	// 启动定期刷新
	go logger.flushLoop()

	return logger, nil
}

// Log 记录审计事件
func (l *Logger) Log(event Event) {
	if !l.enabled {
		return
	}

	// 设置时间戳
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	l.mu.Lock()
	l.buffer = append(l.buffer, event)
	l.eventCount++
	shouldFlush := len(l.buffer) >= l.bufSize
	l.mu.Unlock()

	// 缓冲区满时刷新
	if shouldFlush {
		select {
		case l.flushChan <- struct{}{}:
		default:
		}
	}
}

// LogConnect 记录连接事件
func (l *Logger) LogConnect(clientIP, backend, clientID string) {
	l.Log(Event{
		Type:     EventConnect,
		ClientIP: clientIP,
		Backend:  backend,
		Resource: clientID,
		Action:   "client connected",
		Result:   "success",
	})
}

// LogDisconnect 记录断开连接事件
func (l *Logger) LogDisconnect(clientIP, backend, clientID string, duration time.Duration) {
	l.Log(Event{
		Type:     EventDisconnect,
		ClientIP: clientIP,
		Backend:  backend,
		Resource: clientID,
		Action:   "client disconnected",
		Result:   "success",
		Duration: duration,
	})
}

// LogAuthSuccess 记录认证成功
func (l *Logger) LogAuthSuccess(clientIP, backend, tokenPrefix string) {
	l.Log(Event{
		Type:     EventAuthSuccess,
		ClientIP: clientIP,
		Backend:  backend,
		Action:   "authentication successful",
		Result:   "success",
		Details:  map[string]interface{}{"token_prefix": tokenPrefix},
	})
}

// LogAuthFailure 记录认证失败
func (l *Logger) LogAuthFailure(clientIP, reason string) {
	l.Log(Event{
		Type:     EventAuthFailure,
		ClientIP: clientIP,
		Action:   "authentication failed",
		Result:   "failure",
		Error:    reason,
	})
}

// LogBackendCreate 记录后端创建
func (l *Logger) LogBackendCreate(backendName, host string, port int) {
	l.Log(Event{
		Type:     EventBackendCreate,
		Resource: backendName,
		Action:   "backend created",
		Result:   "success",
		Details: map[string]interface{}{
			"host": host,
			"port": port,
		},
	})
}

// LogBackendDelete 记录后端删除
func (l *Logger) LogBackendDelete(backendName string) {
	l.Log(Event{
		Type:     EventBackendDelete,
		Resource: backendName,
		Action:   "backend deleted",
		Result:   "success",
	})
}

// LogIPBlocked 记录 IP 被阻止
func (l *Logger) LogIPBlocked(clientIP, reason string) {
	l.Log(Event{
		Type:     EventIPBlocked,
		ClientIP: clientIP,
		Action:   "IP blocked",
		Result:   "denied",
		Error:    reason,
	})
}

// LogRateLimited 记录限流触发
func (l *Logger) LogRateLimited(clientIP string, currentCount, maxCount int) {
	l.Log(Event{
		Type:     EventRateLimited,
		ClientIP: clientIP,
		Action:   "rate limit exceeded",
		Result:   "denied",
		Details: map[string]interface{}{
			"current_count": currentCount,
			"max_count":     maxCount,
		},
	})
}

// LogError 记录错误
func (l *Logger) LogError(err error, details map[string]interface{}) {
	l.Log(Event{
		Type:    EventError,
		Action:  "error occurred",
		Result:  "failure",
		Error:   err.Error(),
		Details: details,
	})
}

// LogConfigReload 记录配置重载
func (l *Logger) LogConfigReload(success bool, changes []string) {
	result := "success"
	if !success {
		result = "failure"
	}
	l.Log(Event{
		Type:     EventConfigReload,
		Action:   "configuration reloaded",
		Result:   result,
		Details:  map[string]interface{}{"changes": changes},
	})
}

// LogShutdown 记录关闭
func (l *Logger) LogShutdown(reason string) {
	l.Log(Event{
		Type:   EventShutdown,
		Action: "server shutdown",
		Result: "success",
		Details: map[string]interface{}{
			"reason": reason,
		},
	})
}

// Stats 获取统计信息
func (l *Logger) Stats() (eventsLogged, errorsLogged int64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.eventCount, l.errorCount
}

// Flush 刷新缓冲区
func (l *Logger) Flush() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.flushLocked()
}

// flushLocked 刷新缓冲区（调用者必须持有锁）
func (l *Logger) flushLocked() error {
	if len(l.buffer) == 0 {
		return nil
	}

	for _, event := range l.buffer {
		var line string
		if l.format == "json" {
			data, err := json.Marshal(event)
			if err != nil {
				l.errorCount++
				continue
			}
			line = string(data)
		} else {
			line = formatText(event)
		}

		if _, err := l.file.WriteString(line + "\n"); err != nil {
			l.errorCount++
			return err
		}
	}

	// 清空缓冲
	l.buffer = l.buffer[:0]
	return l.file.Sync()
}

// Close 关闭审计日志
func (l *Logger) Close() error {
	// 停止 flushLoop
	close(l.stopChan)

	// 最后一次刷新
	if err := l.Flush(); err != nil {
		return err
	}

	if l.file != nil {
		return l.file.Close()
	}
	return nil
}

// Enable 启用审计日志
func (l *Logger) Enable() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.enabled = true
}

// Disable 禁用审计日志
func (l *Logger) Disable() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.enabled = false
}

// IsEnabled 检查是否启用
func (l *Logger) IsEnabled() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.enabled
}

// Rotate 轮转日志文件
func (l *Logger) Rotate() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	// 刷新现有内容
	if err := l.flushLocked(); err != nil {
		return err
	}

	// 关闭旧文件
	if l.file != nil {
		if err := l.file.Close(); err != nil {
			return err
		}
	}

	// 重命名旧文件
	backup := l.filePath + "." + time.Now().Format("20060102-150405")
	if err := os.Rename(l.filePath, backup); err != nil && !os.IsNotExist(err) {
		return err
	}

	// 打开新文件
	file, err := os.OpenFile(l.filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	l.file = file

	return nil
}

// flushLoop 定期刷新
func (l *Logger) flushLoop() {
	ticker := time.NewTicker(l.flushInt)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			l.Flush()
		case <-l.flushChan:
			l.Flush()
		case <-l.stopChan:
			return
		}
	}
}

// formatText 格式化为文本
func formatText(event Event) string {
	base := fmt.Sprintf("[%s] %s %s",
		event.Timestamp.Format(time.RFC3339),
		event.Type,
		event.Action,
	)

	if event.ClientIP != "" {
		base += fmt.Sprintf(" client=%s", event.ClientIP)
	}
	if event.Backend != "" {
		base += fmt.Sprintf(" backend=%s", event.Backend)
	}
	if event.Resource != "" {
		base += fmt.Sprintf(" resource=%s", event.Resource)
	}
	if event.Result != "" {
		base += fmt.Sprintf(" result=%s", event.Result)
	}
	if event.Error != "" {
		base += fmt.Sprintf(" error=%s", event.Error)
	}
	if event.Duration > 0 {
		base += fmt.Sprintf(" duration=%s", event.Duration)
	}

	return base
}

// NoopLogger 无操作审计日志记录器
type NoopLogger struct{}

// NewNoopLogger 创建无操作审计日志记录器
func NewNoopLogger() *NoopLogger {
	return &NoopLogger{}
}

func (l *NoopLogger) Log(event Event)                                          {}
func (l *NoopLogger) LogConnect(clientIP, backend, clientID string)            {}
func (l *NoopLogger) LogDisconnect(clientIP, backend, clientID string, d time.Duration) {}
func (l *NoopLogger) LogAuthSuccess(clientIP, backend, tokenPrefix string)     {}
func (l *NoopLogger) LogAuthFailure(clientIP, reason string)                   {}
func (l *NoopLogger) LogBackendCreate(backendName, host string, port int)      {}
func (l *NoopLogger) LogBackendDelete(backendName string)                      {}
func (l *NoopLogger) LogIPBlocked(clientIP, reason string)                     {}
func (l *NoopLogger) LogRateLimited(clientIP string, currentCount, maxCount int) {}
func (l *NoopLogger) LogError(err error, details map[string]interface{})       {}
func (l *NoopLogger) LogConfigReload(success bool, changes []string)           {}
func (l *NoopLogger) LogShutdown(reason string)                                {}
func (l *NoopLogger) Flush() error                                             { return nil }
func (l *NoopLogger) Close() error                                             { return nil }
func (l *NoopLogger) Stats() (eventsLogged, errorsLogged int64)                { return 0, 0 }
func (l *NoopLogger) IsEnabled() bool                                          { return false }
func (l *NoopLogger) Rotate() error                                            { return nil }