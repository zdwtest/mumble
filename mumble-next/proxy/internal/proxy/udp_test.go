package proxy

import (
	"net"
	"testing"
	"time"
)

func TestNewUDPProxy(t *testing.T) {
	proxy := NewUDPProxy("127.0.0.1", 64738, 0)
	if proxy == nil {
		t.Fatal("expected UDP proxy to be created")
	}

	if proxy.murmurHost != "127.0.0.1" {
		t.Errorf("expected murmurHost 127.0.0.1, got %s", proxy.murmurHost)
	}

	if proxy.murmurPort != 64738 {
		t.Errorf("expected murmurPort 64738, got %d", proxy.murmurPort)
	}
}

func TestUDPProxy_StartStop(t *testing.T) {
	// 找一个可用端口
	listener, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("failed to find available port: %v", err)
	}
	addr := listener.LocalAddr().(*net.UDPAddr)
	listener.Close()

	proxy := NewUDPProxy("127.0.0.1", 64738, addr.Port)

	// 启动代理
	if err := proxy.Start(); err != nil {
		t.Fatalf("failed to start UDP proxy: %v", err)
	}

	// 验证正在运行
	if !proxy.running.Load() {
		t.Error("expected proxy to be running")
	}

	// 停止代理
	proxy.Stop()

	// 验证已停止
	if proxy.running.Load() {
		t.Error("expected proxy to be stopped")
	}
}

func TestUDPProxy_GetStats(t *testing.T) {
	proxy := NewUDPProxy("127.0.0.1", 64738, 0)

	stats := proxy.GetStats()

	if stats.PacketsReceived.Load() != 0 {
		t.Errorf("expected 0 packets received, got %d", stats.PacketsReceived.Load())
	}

	if stats.PacketsSent.Load() != 0 {
		t.Errorf("expected 0 packets sent, got %d", stats.PacketsSent.Load())
	}
}

func TestUDPProxy_GetClientCount(t *testing.T) {
	proxy := NewUDPProxy("127.0.0.1", 64738, 0)

	// 初始客户端数量应为 0
	if count := proxy.GetClientCount(); count != 0 {
		t.Errorf("expected 0 clients, got %d", count)
	}

	// 添加一些客户端
	proxy.clients.Store("192.168.1.1:12345", time.Now())
	proxy.clients.Store("192.168.1.2:12345", time.Now())

	if count := proxy.GetClientCount(); count != 2 {
		t.Errorf("expected 2 clients, got %d", count)
	}
}

func TestUDPProxy_CleanupStaleClients(t *testing.T) {
	proxy := NewUDPProxy("127.0.0.1", 64738, 0)

	// 添加一个过期客户端和一个活跃客户端
	proxy.clients.Store("old_client", time.Now().Add(-10*time.Minute))
	proxy.clients.Store("new_client", time.Now())

	// 清理超过 5 分钟的客户端
	proxy.CleanupStaleClients(5 * time.Minute)

	// 验证只有活跃客户端存在
	if count := proxy.GetClientCount(); count != 1 {
		t.Errorf("expected 1 client after cleanup, got %d", count)
	}
}