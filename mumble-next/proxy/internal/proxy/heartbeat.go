package proxy

import (
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

// HeartbeatConfig 心跳配置
type HeartbeatConfig struct {
	// PingInterval 发送 Ping 的间隔
	PingInterval time.Duration
	// PongWait 等待 Pong 的超时时间
	PongWait time.Duration
	// MaxMissedPongs 最大错过 Pong 次数
	MaxMissedPongs int
}

// DefaultHeartbeatConfig 默认心跳配置
func DefaultHeartbeatConfig() HeartbeatConfig {
	return HeartbeatConfig{
		PingInterval:   30 * time.Second,
		PongWait:       10 * time.Second,
		MaxMissedPongs: 3,
	}
}

// HeartbeatManager 心跳管理器
type HeartbeatManager struct {
	config      HeartbeatConfig
	conn        *websocket.Conn
	logger      *zap.Logger
	stopCh      chan struct{}
	doneCh      chan struct{}

	// 统计
	mu            sync.Mutex
	pingsSent     int64
	pongsReceived int64
	missedPongs   int

	// 回调
	onTimeout func()
}

// NewHeartbeatManager 创建心跳管理器
func NewHeartbeatManager(conn *websocket.Conn, config HeartbeatConfig, logger *zap.Logger) *HeartbeatManager {
	return &HeartbeatManager{
		config: config,
		conn:   conn,
		logger: logger,
		stopCh: make(chan struct{}),
		doneCh: make(chan struct{}),
	}
}

// Start 启动心跳
func (h *HeartbeatManager) Start() {
	go h.loop()
}

// Stop 停止心跳
func (h *HeartbeatManager) Stop() {
	close(h.stopCh)
	<-h.doneCh
}

// OnTimeout 设置超时回调
func (h *HeartbeatManager) OnTimeout(callback func()) {
	h.onTimeout = callback
}

// Stats 获取心跳统计
func (h *HeartbeatManager) Stats() (pingsSent, pongsReceived int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.pingsSent, h.pongsReceived
}

func (h *HeartbeatManager) loop() {
	defer close(h.doneCh)

	ticker := time.NewTicker(h.config.PingInterval)
	defer ticker.Stop()

	// 设置 Pong 处理器
	h.conn.SetPongHandler(func(appData string) error {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.pongsReceived++
		h.missedPongs = 0
		h.logger.Debug("pong received")
		return nil
	})

	for {
		select {
		case <-h.stopCh:
			return
		case <-ticker.C:
			if err := h.sendPing(); err != nil {
				h.logger.Debug("ping failed", zap.Error(err))
				return
			}
		}
	}
}

func (h *HeartbeatManager) sendPing() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.pingsSent++

	// 检查是否错过太多 Pong
	if h.pongsReceived < h.pingsSent-int64(h.missedPongs) {
		h.missedPongs++
		if h.missedPongs >= h.config.MaxMissedPongs {
			h.logger.Warn("too many missed pongs, closing connection",
				zap.Int("missed", h.missedPongs),
			)
			if h.onTimeout != nil {
				go h.onTimeout()
			}
			return websocket.ErrCloseSent
		}
	}

	// 发送 Ping
	if err := h.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
		return err
	}

	h.logger.Debug("ping sent",
		zap.Int64("total_pings", h.pingsSent),
		zap.Int("missed_pongs", h.missedPongs),
	)
	return nil
}

// KeepAlive 保持连接活跃
type KeepAlive struct {
	conn        *websocket.Conn
	interval    time.Duration
	timeout     time.Duration
	logger      *zap.Logger
	stopCh      chan struct{}
	lastPong    time.Time
	mu          sync.Mutex
}

// NewKeepAlive 创建保活器
func NewKeepAlive(conn *websocket.Conn, interval, timeout time.Duration, logger *zap.Logger) *KeepAlive {
	return &KeepAlive{
		conn:     conn,
		interval: interval,
		timeout:  timeout,
		logger:   logger,
		stopCh:   make(chan struct{}),
	}
}

// Start 启动保活
func (k *KeepAlive) Start() {
	k.mu.Lock()
	k.lastPong = time.Now()
	k.mu.Unlock()

	// 设置 Pong 处理器
	k.conn.SetPongHandler(func(string) error {
		k.mu.Lock()
		k.lastPong = time.Now()
		k.mu.Unlock()
		return nil
	})

	go k.loop()
}

// Stop 停止保活
func (k *KeepAlive) Stop() {
	close(k.stopCh)
}

// IsAlive 检查连接是否活跃
func (k *KeepAlive) IsAlive() bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	return time.Since(k.lastPong) < k.timeout
}

func (k *KeepAlive) loop() {
	ticker := time.NewTicker(k.interval)
	defer ticker.Stop()

	for {
		select {
		case <-k.stopCh:
			return
		case <-ticker.C:
			// 检查是否超时
			if !k.IsAlive() {
				k.logger.Warn("connection keep-alive timeout")
				k.conn.Close()
				return
			}

			// 发送 Ping
			if err := k.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				k.logger.Debug("ping failed", zap.Error(err))
				return
			}
		}
	}
}