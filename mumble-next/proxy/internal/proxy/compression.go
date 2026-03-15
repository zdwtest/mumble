package proxy

import (
	"bytes"
	"compress/flate"
	"io"
	"sync"

	"github.com/gorilla/websocket"
)

// CompressionConfig WebSocket 压缩配置
type CompressionConfig struct {
	// Enabled 是否启用压缩
	Enabled bool

	// Level 压缩级别 (flate.NoCompression to flate.BestCompression)
	Level int

	// Threshold 压缩阈值，消息大于此值才压缩 (bytes)
	Threshold int

	// ContextTakeover 服务端是否保留上下文
	ContextTakeover bool

	// ClientContextTakeover 客户端是否保留上下文
	ClientContextTakeover bool
}

// DefaultCompressionConfig 默认压缩配置
func DefaultCompressionConfig() CompressionConfig {
	return CompressionConfig{
		Enabled:               true,
		Level:                 flate.DefaultCompression,
		Threshold:             256,  // 256 bytes
		ContextTakeover:       true, // 保留上下文以获得更好压缩率
		ClientContextTakeover: true,
	}
}

// Compressor WebSocket 消息压缩器
type Compressor struct {
	config    CompressionConfig
	flatePool sync.Pool
	bufPool   sync.Pool
}

// NewCompressor 创建压缩器
func NewCompressor(config CompressionConfig) *Compressor {
	return &Compressor{
		config: config,
		flatePool: sync.Pool{
			New: func() interface{} {
				w, _ := flate.NewWriter(nil, config.Level)
				return w
			},
		},
		bufPool: sync.Pool{
			New: func() interface{} {
				return new(bytes.Buffer)
			},
		},
	}
}

// ShouldCompress 判断是否应该压缩消息
func (c *Compressor) ShouldCompress(data []byte) bool {
	if !c.config.Enabled {
		return false
	}
	return len(data) >= c.config.Threshold
}

// Compress 压缩数据
func (c *Compressor) Compress(data []byte) ([]byte, error) {
	if !c.config.Enabled {
		return data, nil
	}

	// 从池中获取 writer 和 buffer
	w := c.flatePool.Get().(*flate.Writer)
	defer c.flatePool.Put(w)

	buf := c.bufPool.Get().(*bytes.Buffer)
	defer func() {
		buf.Reset()
		c.bufPool.Put(buf)
	}()

	w.Reset(buf)
	if _, err := w.Write(data); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}

	// 复制数据
	result := make([]byte, buf.Len())
	copy(result, buf.Bytes())
	return result, nil
}

// GetCompressionLevel 获取压缩级别
func (c *Compressor) GetCompressionLevel() int {
	return c.config.Level
}

// IsEnabled 检查是否启用
func (c *Compressor) IsEnabled() bool {
	return c.config.Enabled
}

// SetLevel 设置压缩级别
func (c *Compressor) SetLevel(level int) error {
	// Valid levels: -2 (HuffmanOnly), -1 (DefaultCompression), 0 (NoCompression), 1-9
	// Note: flate package constants may vary by Go version, but -2 to 9 is the valid range
	if level < -2 || level > flate.BestCompression {
		return ErrInvalidCompressionLevel
	}
	c.config.Level = level
	return nil
}

// SetThreshold 设置压缩阈值
func (c *Compressor) SetThreshold(threshold int) {
	c.config.Threshold = threshold
}

// Decompressor WebSocket 消息解压器
type Decompressor struct {
	flatePool sync.Pool
	bufPool   sync.Pool
}

// NewDecompressor 创建解压器
func NewDecompressor() *Decompressor {
	return &Decompressor{
		flatePool: sync.Pool{
			New: func() interface{} {
				return flate.NewReader(nil)
			},
		},
		bufPool: sync.Pool{
			New: func() interface{} {
				return new(bytes.Buffer)
			},
		},
	}
}

// Decompress 解压数据
func (d *Decompressor) Decompress(data []byte) ([]byte, error) {
	r := d.flatePool.Get().(io.ReadCloser)
	defer d.flatePool.Put(r)

	if err := r.(flate.Resetter).Reset(bytes.NewReader(data), nil); err != nil {
		return nil, err
	}
	defer r.Close()

	buf := d.bufPool.Get().(*bytes.Buffer)
	defer func() {
		buf.Reset()
		d.bufPool.Put(buf)
	}()

	if _, err := io.Copy(buf, r); err != nil {
		return nil, err
	}

	result := make([]byte, buf.Len())
	copy(result, buf.Bytes())
	return result, nil
}

// CompressionUpgrader WebSocket 压缩升级器
type CompressionUpgrader struct {
	upgrader websocket.Upgrader
	config   CompressionConfig
}

// NewCompressionUpgrader 创建压缩升级器
func NewCompressionUpgrader(config CompressionConfig) *CompressionUpgrader {
	// 创建支持压缩的 upgrader
	u := websocket.Upgrader{
		ReadBufferSize:  4096,
		WriteBufferSize: 4096,
	}

	return &CompressionUpgrader{
		upgrader: u,
		config:   config,
	}
}

// GetUpgrader 获取 websocket.Upgrader
func (u *CompressionUpgrader) GetUpgrader() websocket.Upgrader {
	return u.upgrader
}

// EnableCompression 为连接启用压缩
func (u *CompressionUpgrader) EnableCompression(conn *websocket.Conn) {
	if u.config.Enabled {
		conn.EnableWriteCompression(true)
		conn.SetCompressionLevel(u.config.Level)
	}
}

// CompressionStats 压缩统计
type CompressionStats struct {
	// TotalCompressed 已压缩的消息数
	TotalCompressed int64

	// TotalUncompressed 未压缩的消息数
	TotalUncompressed int64

	// TotalBytesBefore 压缩前的总字节数
	TotalBytesBefore int64

	// TotalBytesAfter 压缩后的总字节数
	TotalBytesAfter int64

	// AverageRatio 平均压缩率
	AverageRatio float64
}

// CompressionTracker 压缩统计跟踪器
type CompressionTracker struct {
	stats CompressionStats
	mu    sync.RWMutex
}

// NewCompressionTracker 创建压缩统计跟踪器
func NewCompressionTracker() *CompressionTracker {
	return &CompressionTracker{}
}

// Record 记录压缩结果
func (t *CompressionTracker) Record(beforeLen, afterLen int, compressed bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if compressed {
		t.stats.TotalCompressed++
		t.stats.TotalBytesBefore += int64(beforeLen)
		t.stats.TotalBytesAfter += int64(afterLen)
	} else {
		t.stats.TotalUncompressed++
	}
}

// GetStats 获取统计信息
func (t *CompressionTracker) GetStats() CompressionStats {
	t.mu.RLock()
	defer t.mu.RUnlock()

	stats := t.stats
	if stats.TotalBytesBefore > 0 {
		stats.AverageRatio = float64(stats.TotalBytesAfter) / float64(stats.TotalBytesBefore)
	}
	return stats
}

// Reset 重置统计
func (t *CompressionTracker) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.stats = CompressionStats{}
}

// 错误定义
var (
	ErrInvalidCompressionLevel = &CompressionError{Msg: "invalid compression level"}
)

// CompressionError 压缩错误
type CompressionError struct {
	Msg string
}

func (e *CompressionError) Error() string {
	return e.Msg
}