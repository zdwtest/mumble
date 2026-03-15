package admin

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

// LogEntry 日志条目
type LogEntry struct {
	// Timestamp 时间戳
	Timestamp time.Time `json:"timestamp"`

	// Level 日志级别
	Level string `json:"level"` // debug, info, warn, error

	// Message 消息
	Message string `json:"message"`

	// Fields 字段
	Fields map[string]interface{} `json:"fields,omitempty"`

	// Source 来源
	Source string `json:"source,omitempty"`
}

// LogStreamer 日志流
type LogStreamer struct {
	clients    map[*websocket.Conn]*logClientConfig
	mu         sync.RWMutex
	buffer     []*LogEntry
	bufferSize int
	logger     *zap.Logger

	// 入站日志通道
	logChan chan *LogEntry

	// 停止信号
	stopChan chan struct{}
}

type logClientConfig struct {
	levelFilter map[string]bool
	sources     []string // 空表示接受所有来源
}

// NewLogStreamer 创建日志流
func NewLogStreamer(bufferSize int, logger *zap.Logger) *LogStreamer {
	if logger == nil {
		logger = zap.NewNop()
	}

	ls := &LogStreamer{
		clients:    make(map[*websocket.Conn]*logClientConfig),
		buffer:     make([]*LogEntry, 0, bufferSize),
		bufferSize: bufferSize,
		logger:     logger,
		logChan:    make(chan *LogEntry, 1000),
		stopChan:   make(chan struct{}),
	}

	go ls.run()
	return ls
}

// WriteLog 写入日志
func (ls *LogStreamer) WriteLog(entry *LogEntry) {
	if entry == nil {
		return
	}
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now()
	}

	select {
	case ls.logChan <- entry:
	default:
		// 通道满，丢弃
		ls.logger.Debug("log channel full, dropping entry")
	}
}

// Write 写入日志 (实现 io.Writer 接口)
func (ls *LogStreamer) Write(p []byte) (n int, err error) {
	ls.WriteLog(&LogEntry{
		Level:   "info",
		Message: string(p),
	})
	return len(p), nil
}

// run 运行日志分发
func (ls *LogStreamer) run() {
	for {
		select {
		case entry := <-ls.logChan:
			ls.processEntry(entry)
		case <-ls.stopChan:
			return
		}
	}
}

// processEntry 处理日志条目
func (ls *LogStreamer) processEntry(entry *LogEntry) {
	ls.mu.Lock()
	defer ls.mu.Unlock()

	// 添加到缓冲区
	ls.buffer = append(ls.buffer, entry)
	if len(ls.buffer) > ls.bufferSize {
		ls.buffer = ls.buffer[1:]
	}

	// 广播给客户端
	for conn, config := range ls.clients {
		if !config.levelFilter[entry.Level] {
			continue
		}
		if len(config.sources) > 0 {
			found := false
			for _, src := range config.sources {
				if src == entry.Source {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}

		data, err := json.Marshal(entry)
		if err != nil {
			continue
		}

		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			conn.Close()
			delete(ls.clients, conn)
		}
	}
}

// AddClient 添加客户端
func (ls *LogStreamer) AddClient(conn *websocket.Conn, levels []string, sources []string) {
	ls.mu.Lock()
	defer ls.mu.Unlock()

	config := &logClientConfig{
		levelFilter: make(map[string]bool),
		sources:     sources,
	}

	// 如果没有指定级别，接受所有级别
	if len(levels) == 0 {
		levels = []string{"debug", "info", "warn", "error"}
	}

	for _, level := range levels {
		config.levelFilter[level] = true
	}

	ls.clients[conn] = config

	// 发送缓冲的日志
	for _, entry := range ls.buffer {
		if !config.levelFilter[entry.Level] {
			continue
		}
		if len(config.sources) > 0 {
			found := false
			for _, src := range config.sources {
				if src == entry.Source {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}

		data, _ := json.Marshal(entry)
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			break
		}
	}
}

// RemoveClient 移除客户端
func (ls *LogStreamer) RemoveClient(conn *websocket.Conn) {
	ls.mu.Lock()
	defer ls.mu.Unlock()

	delete(ls.clients, conn)
	conn.Close()
}

// ClientCount 获取客户端数量
func (ls *LogStreamer) ClientCount() int {
	ls.mu.RLock()
	defer ls.mu.RUnlock()
	return len(ls.clients)
}

// Close 关闭日志流
func (ls *LogStreamer) Close() {
	close(ls.stopChan)

	ls.mu.Lock()
	defer ls.mu.Unlock()

	for conn := range ls.clients {
		conn.Close()
	}
	ls.clients = make(map[*websocket.Conn]*logClientConfig)
}

// GetBuffer 获取缓冲区内容
func (ls *LogStreamer) GetBuffer() []*LogEntry {
	ls.mu.RLock()
	defer ls.mu.RUnlock()

	result := make([]*LogEntry, len(ls.buffer))
	copy(result, ls.buffer)
	return result
}

// LogStreamHandler 日志流 HTTP 处理器
type LogStreamHandler struct {
	streamer *LogStreamer
	upgrader websocket.Upgrader
	logger   *zap.Logger
}

// NewLogStreamHandler 创建日志流处理器
func NewLogStreamHandler(streamer *LogStreamer, logger *zap.Logger) *LogStreamHandler {
	if logger == nil {
		logger = zap.NewNop()
	}

	return &LogStreamHandler{
		streamer: streamer,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 4096,
		},
		logger: logger,
	}
}

// ServeHTTP 处理 HTTP 请求
func (h *LogStreamHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.logger.Debug("failed to upgrade connection", zap.Error(err))
		return
	}

	// 解析查询参数
	levels := parseStringSlice(r.URL.Query().Get("levels"))
	sources := parseStringSlice(r.URL.Query().Get("sources"))

	h.streamer.AddClient(conn, levels, sources)
	defer h.streamer.RemoveClient(conn)

	// 读取客户端消息 (用于控制命令)
	reader := bufio.NewReader(conn.UnderlyingConn())
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		line = line[:len(line)-1] // Remove newline

		// 处理控制命令
		var cmd struct {
			Command string   `json:"command"`
			Levels  []string `json:"levels,omitempty"`
			Sources []string `json:"sources,omitempty"`
		}

		if err := json.Unmarshal([]byte(line), &cmd); err != nil {
			continue
		}

		switch cmd.Command {
		case "set_levels":
			h.streamer.RemoveClient(conn)
			h.streamer.AddClient(conn, cmd.Levels, sources)
		case "set_sources":
			h.streamer.RemoveClient(conn)
			h.streamer.AddClient(conn, levels, cmd.Sources)
		}
	}
}

func parseStringSlice(s string) []string {
	if s == "" {
		return nil
	}

	var result []string
	if err := json.Unmarshal([]byte(s), &result); err != nil {
		// 尝试作为逗号分隔
		for _, part := range splitByComma(s) {
			if part != "" {
				result = append(result, part)
			}
		}
	}
	return result
}

func splitByComma(s string) []string {
	var result []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == ',' {
			result = append(result, s[start:i])
			start = i + 1
		}
	}
	result = append(result, s[start:])
	return result
}

// RegisterRoutes 注册路由
func (h *LogStreamHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/logs/stream", h.ServeHTTP)
	mux.HandleFunc("/api/logs", h.handleBuffer)
}

// handleBuffer 处理缓冲日志请求
func (h *LogStreamHandler) handleBuffer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	buffer := h.streamer.GetBuffer()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"entries": buffer,
		"count":   len(buffer),
	})
}

// ZapLogBridge 将 zap 日志桥接到 LogStreamer
type ZapLogBridge struct {
	streamer *LogStreamer
	level    string
	source   string
}

// NewZapLogBridge 创建 zap 日志桥接
func NewZapLogBridge(streamer *LogStreamer, level, source string) *ZapLogBridge {
	return &ZapLogBridge{
		streamer: streamer,
		level:    level,
		source:   source,
	}
}

// Write 实现 io.Writer 接口
func (b *ZapLogBridge) Write(p []byte) (n int, err error) {
	b.streamer.WriteLog(&LogEntry{
		Level:   b.level,
		Message: string(p),
		Source:  b.source,
	})
	return len(p), nil
}

// String 返回格式化字符串
func (b *ZapLogBridge) String() string {
	return fmt.Sprintf("ZapLogBridge{level=%s, source=%s}", b.level, b.source)
}