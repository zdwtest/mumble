package proxy

import (
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// RateLimiter 速率限制器
type RateLimiter struct {
	connections sync.Map // IP -> *int32
	maxPerIP    int
}

// NewRateLimiter 创建速率限制器
func NewRateLimiter(maxPerIP int) *RateLimiter {
	return &RateLimiter{
		maxPerIP: maxPerIP,
	}
}

// Allow 检查是否允许连接
func (r *RateLimiter) Allow(ip string) bool {
	if r.maxPerIP <= 0 {
		return true // 未启用限制
	}

	val, _ := r.connections.LoadOrStore(ip, new(int32))
	count := val.(*int32)

	if atomic.LoadInt32(count) >= int32(r.maxPerIP) {
		return false
	}

	atomic.AddInt32(count, 1)
	return true
}

// Release 释放连接计数
func (r *RateLimiter) Release(ip string) {
	val, ok := r.connections.Load(ip)
	if !ok {
		return
	}

	count := val.(*int32)
	if atomic.AddInt32(count, -1) <= 0 {
		r.connections.Delete(ip)
	}
}

// GetCount 获取指定 IP 的连接数
func (r *RateLimiter) GetCount(ip string) int {
	val, ok := r.connections.Load(ip)
	if !ok {
		return 0
	}
	return int(atomic.LoadInt32(val.(*int32)))
}

// TCPConn TCP 连接管理器
type TCPConn struct {
	conn    net.Conn
	bufPool *sync.Pool
}

// NewTCPConn 创建 TCP 连接
func NewTCPConn(host string, port int, timeout time.Duration) (*TCPConn, error) {
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))

	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to %s: %w", addr, err)
	}

	return &TCPConn{
		conn: conn,
		bufPool: &sync.Pool{
			New: func() interface{} {
				return make([]byte, 32*1024)
			},
		},
	}, nil
}

// Read 读取数据
func (c *TCPConn) Read() ([]byte, error) {
	buf := c.bufPool.Get().([]byte)
	defer c.bufPool.Put(buf)

	n, err := c.conn.Read(buf)
	if err != nil {
		return nil, err
	}

	// 返回数据的副本
	data := make([]byte, n)
	copy(data, buf[:n])
	return data, nil
}

// Write 写入数据
func (c *TCPConn) Write(data []byte) (int, error) {
	return c.conn.Write(data)
}

// Close 关闭连接
func (c *TCPConn) Close() error {
	return c.conn.Close()
}

// RemoteAddr 获取远程地址
func (c *TCPConn) RemoteAddr() net.Addr {
	return c.conn.RemoteAddr()
}

// LocalAddr 获取本地地址
func (c *TCPConn) LocalAddr() net.Addr {
	return c.conn.LocalAddr()
}

// SetDeadline 设置截止时间
func (c *TCPConn) SetDeadline(t time.Time) error {
	return c.conn.SetDeadline(t)
}

// Forwarder 数据转发器
type Forwarder struct {
	wsConn  *websocket.Conn
	tcpConn net.Conn
	bufSize int
}

// NewForwarder 创建转发器
func NewForwarder(wsConn *websocket.Conn, tcpConn net.Conn, bufSize int) *Forwarder {
	return &Forwarder{
		wsConn:  wsConn,
		tcpConn: tcpConn,
		bufSize: bufSize,
	}
}

// ForwardWebSocketToTCP 从 WebSocket 转发到 TCP
func (f *Forwarder) ForwardWebSocketToTCP(onError func(error)) {
	for {
		_, data, err := f.wsConn.ReadMessage()
		if err != nil {
			onError(err)
			return
		}

		if _, err := f.tcpConn.Write(data); err != nil {
			onError(err)
			return
		}
	}
}

// ForwardTCPToWebSocket 从 TCP 转发到 WebSocket
func (f *Forwarder) ForwardTCPToWebSocket(onError func(error)) {
	buf := make([]byte, f.bufSize)

	for {
		n, err := f.tcpConn.Read(buf)
		if err != nil {
			onError(err)
			return
		}

		if err := f.wsConn.WriteMessage(websocket.BinaryMessage, buf[:n]); err != nil {
			onError(err)
			return
		}
	}
}