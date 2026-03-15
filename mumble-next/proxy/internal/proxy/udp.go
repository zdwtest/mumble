package proxy

import (
	"fmt"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// UDPProxy UDP 代理
type UDPProxy struct {
	murmurHost string
	murmurPort int
	listenPort int
	conn       *net.UDPConn
	clients    sync.Map // clientAddr -> lastActivity
	stats      UDPStats
	stopCh     chan struct{}
	running    atomic.Bool
}

// UDPStats UDP 统计
type UDPStats struct {
	PacketsReceived atomic.Int64
	PacketsSent     atomic.Int64
	BytesReceived   atomic.Int64
	BytesSent       atomic.Int64
}

// NewUDPProxy 创建 UDP 代理
func NewUDPProxy(murmurHost string, murmurPort, listenPort int) *UDPProxy {
	return &UDPProxy{
		murmurHost: murmurHost,
		murmurPort: murmurPort,
		listenPort: listenPort,
		stopCh:     make(chan struct{}),
	}
}

// Start 启动 UDP 代理
func (p *UDPProxy) Start() error {
	listenAddr := &net.UDPAddr{Port: p.listenPort}
	conn, err := net.ListenUDP("udp", listenAddr)
	if err != nil {
		return fmt.Errorf("failed to listen UDP on port %d: %w", p.listenPort, err)
	}
	p.conn = conn
	p.running.Store(true)

	murmurAddr := &net.UDPAddr{
		IP:   net.ParseIP(p.murmurHost),
		Port: p.murmurPort,
	}

	log.Printf("UDP proxy started: :%d -> %s", p.listenPort, murmurAddr)

	go p.forwardLoop(murmurAddr)

	return nil
}

// Stop 停止 UDP 代理
func (p *UDPProxy) Stop() {
	p.running.Store(false)
	close(p.stopCh)
	if p.conn != nil {
		p.conn.Close()
	}
}

// forwardLoop 转发循环
func (p *UDPProxy) forwardLoop(murmurAddr *net.UDPAddr) {
	buf := make([]byte, 2048)

	for p.running.Load() {
		n, clientAddr, err := p.conn.ReadFromUDP(buf)
		if err != nil {
			if p.running.Load() {
				log.Printf("UDP read error: %v", err)
			}
			continue
		}

		p.stats.PacketsReceived.Add(1)
		p.stats.BytesReceived.Add(int64(n))

		// 记录客户端活动
		p.clients.Store(clientAddr.String(), time.Now())

		// 转发到 Murmur
		wn, err := p.conn.WriteToUDP(buf[:n], murmurAddr)
		if err != nil {
			log.Printf("UDP write error: %v", err)
			continue
		}
		p.stats.PacketsSent.Add(1)
		p.stats.BytesSent.Add(int64(wn))
	}
}

// GetStats 获取统计指针
func (p *UDPProxy) GetStats() *UDPStats {
	return &p.stats
}

// GetClientCount 获取客户端数量
func (p *UDPProxy) GetClientCount() int {
	count := 0
	p.clients.Range(func(_, _ interface{}) bool {
		count++
		return true
	})
	return count
}

// CleanupStaleClients 清理过期客户端
func (p *UDPProxy) CleanupStaleClients(timeout time.Duration) {
	now := time.Now()
	p.clients.Range(func(key, value interface{}) bool {
		if lastActivity, ok := value.(time.Time); ok {
			if now.Sub(lastActivity) > timeout {
				p.clients.Delete(key)
			}
		}
		return true
	})
}