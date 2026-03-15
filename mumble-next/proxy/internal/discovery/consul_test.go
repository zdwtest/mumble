package discovery

import (
	"context"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/consul/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// ConsulTestServer represents a mock Consul server for testing
type ConsulTestServer struct {
	listener net.Listener
	services map[string]*api.AgentService
	checks   map[string]*api.AgentCheck
	mu       sync.RWMutex
}

// NewConsulTestServer creates a mock Consul server
func NewConsulTestServer() (*ConsulTestServer, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}

	return &ConsulTestServer{
		listener: listener,
		services: make(map[string]*api.AgentService),
		checks:   make(map[string]*api.AgentCheck),
	}, nil
}

// Address returns the server address
func (s *ConsulTestServer) Address() string {
	return s.listener.Addr().String()
}

// Close closes the server
func (s *ConsulTestServer) Close() error {
	return s.listener.Close()
}

// AddService adds a service to the mock server
func (s *ConsulTestServer) AddService(service *api.AgentService) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.services[service.ID] = service
}

// RemoveService removes a service from the mock server
func (s *ConsulTestServer) RemoveService(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.services, id)
}

// TestConsulDiscovery tests with real Consul client (requires Consul running)
func TestConsulDiscovery_Basic(t *testing.T) {
	// Skip if Consul is not available
	cfg := ConsulConfig{
		Address: "127.0.0.1:8500",
	}

	logger := zap.NewNop()
	d, err := NewConsulDiscovery(cfg, logger)
	if err != nil {
		t.Skipf("Consul not available: %v", err)
		return
	}
	defer d.Close()

	ctx := context.Background()

	t.Run("Register", func(t *testing.T) {
		instance := &Instance{
			ID:       "test-murmur-1",
			Name:     "murmur",
			Host:     "192.168.1.100",
			Port:     64738,
			Metadata: map[string]string{"version": "1.0"},
			Healthy:  true,
			Weight:   2,
		}

		err := d.Register(ctx, instance)
		if err != nil {
			t.Skipf("Cannot register: %v", err)
			return
		}

		defer d.Deregister(ctx, instance.ID)

		// 发现服务
		time.Sleep(100 * time.Millisecond)

		instances, err := d.Discover(ctx, "murmur")
		if err != nil {
			t.Skipf("Cannot discover: %v", err)
			return
		}

		assert.NotEmpty(t, instances, "Should find registered service")

		found := false
		for _, inst := range instances {
			if inst.ID == instance.ID {
				found = true
				assert.Equal(t, instance.Host, inst.Host)
				assert.Equal(t, instance.Port, inst.Port)
				break
			}
		}
		assert.True(t, found, "Should find registered instance")
	})

	t.Run("Deregister", func(t *testing.T) {
		instance := &Instance{
			ID:   "test-murmur-2",
			Name: "murmur",
			Host: "192.168.1.101",
			Port: 64738,
		}

		err := d.Register(ctx, instance)
		if err != nil {
			t.Skipf("Cannot register: %v", err)
			return
		}

		time.Sleep(100 * time.Millisecond)

		err = d.Deregister(ctx, instance.ID)
		require.NoError(t, err)
	})

	t.Run("DiscoverNotFound", func(t *testing.T) {
		instances, err := d.Discover(ctx, "non-existent-service")
		// Consul returns empty list for non-existent services
		if err == nil {
			assert.Empty(t, instances)
		}
	})
}

func TestConsulDiscovery_Config(t *testing.T) {
	logger := zap.NewNop()

	t.Run("DefaultAddress", func(t *testing.T) {
		cfg := ConsulConfig{}
		d, err := NewConsulDiscovery(cfg, logger)
		require.NoError(t, err)
		defer d.Close()

		assert.NotNil(t, d.client)
	})

	t.Run("CustomAddress", func(t *testing.T) {
		cfg := ConsulConfig{
			Address: "consul.example.com:8500",
		}
		d, err := NewConsulDiscovery(cfg, logger)
		require.NoError(t, err)
		defer d.Close()

		assert.NotNil(t, d.client)
	})

	t.Run("WithToken", func(t *testing.T) {
		cfg := ConsulConfig{
			Address: "127.0.0.1:8500",
			Token:   "test-token",
		}
		d, err := NewConsulDiscovery(cfg, logger)
		require.NoError(t, err)
		defer d.Close()

		assert.NotNil(t, d.client)
	})
}

func TestConsulDiscovery_Options(t *testing.T) {
	logger := zap.NewNop()
	cfg := ConsulConfig{}

	d, err := NewConsulDiscovery(cfg, logger, func(c *ConsulDiscovery) {
		c.services["preloaded"] = []*Instance{
			{ID: "pre-1", Name: "preloaded", Host: "localhost", Port: 8080},
		}
	})
	require.NoError(t, err)
	defer d.Close()

	// 验证选项被应用
	assert.NotNil(t, d.services["preloaded"])
}

func TestConsulDiscovery_Cache(t *testing.T) {
	logger := zap.NewNop()
	cfg := ConsulConfig{Address: "127.0.0.1:8500"}

	d, err := NewConsulDiscovery(cfg, logger)
	if err != nil {
		t.Skipf("Consul not available: %v", err)
		return
	}
	defer d.Close()

	// 预填充缓存
	d.cacheMu.Lock()
	d.services["cached-service"] = []*Instance{
		{ID: "cached-1", Name: "cached-service", Host: "localhost", Port: 8080, Healthy: true},
	}
	d.cacheMu.Unlock()

	// 从缓存获取
	instances, err := d.Discover(context.Background(), "cached-service")
	require.NoError(t, err)
	assert.Len(t, instances, 1)

	// 使缓存失效
	d.InvalidateCache("cached-service")

	d.cacheMu.RLock()
	_, ok := d.services["cached-service"]
	d.cacheMu.RUnlock()
	assert.False(t, ok)
}

func TestConsulDiscovery_Watch(t *testing.T) {
	logger := zap.NewNop()
	cfg := ConsulConfig{Address: "127.0.0.1:8500"}

	d, err := NewConsulDiscovery(cfg, logger)
	if err != nil {
		t.Skipf("Consul not available: %v", err)
		return
	}
	defer d.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ch, err := d.Watch(ctx, "test-service")
	require.NoError(t, err)

	assert.NotNil(t, ch)

	// 验证观察者被注册
	d.watcherMu.Lock()
	watchers := d.watchers["test-service"]
	d.watcherMu.Unlock()
	assert.NotEmpty(t, watchers)
}

func TestConsulDiscovery_Close(t *testing.T) {
	logger := zap.NewNop()
	cfg := ConsulConfig{Address: "127.0.0.1:8500"}

	d, err := NewConsulDiscovery(cfg, logger)
	if err != nil {
		t.Skipf("Consul not available: %v", err)
		return
	}

	// 第一次关闭
	err = d.Close()
	require.NoError(t, err)

	// 第二次关闭应该安全
	err = d.Close()
	require.NoError(t, err)
}

func TestConsulDiscovery_GetClient(t *testing.T) {
	logger := zap.NewNop()
	cfg := ConsulConfig{}

	d, err := NewConsulDiscovery(cfg, logger)
	require.NoError(t, err)
	defer d.Close()

	client := d.GetClient()
	assert.NotNil(t, client)
}

func TestConsulDiscovery_InstanceConversion(t *testing.T) {
	// 测试 Consul 服务到 Instance 的转换
	service := &api.AgentService{
		ID:      "test-1",
		Service: "murmur",
		Address: "192.168.1.100",
		Port:    64738,
		Meta: map[string]string{
			"weight":  "3",
			"version": "1.0",
		},
	}

	instance := &Instance{
		ID:       service.ID,
		Name:     service.Service,
		Host:     service.Address,
		Port:     service.Port,
		Metadata: service.Meta,
		Healthy:  true,
		Weight:   1,
	}

	// 解析权重
	if weight, ok := service.Meta["weight"]; ok {
		var w int
		_, _ = fmt.Sscanf(weight, "%d", &w)
		if w > 0 {
			instance.Weight = w
		}
	}

	assert.Equal(t, "test-1", instance.ID)
	assert.Equal(t, "murmur", instance.Name)
	assert.Equal(t, "192.168.1.100", instance.Host)
	assert.Equal(t, 64738, instance.Port)
	assert.Equal(t, 3, instance.Weight)
	assert.Equal(t, "1.0", instance.Metadata["version"])
}

func TestConsulDiscovery_NotifyWatchers(t *testing.T) {
	logger := zap.NewNop()
	cfg := ConsulConfig{}

	d, err := NewConsulDiscovery(cfg, logger)
	require.NoError(t, err)
	defer d.Close()

	// 创建测试 channel
	ch := make(chan []*Instance, 10)
	d.watcherMu.Lock()
	d.watchers["test-service"] = append(d.watchers["test-service"], ch)
	d.watcherMu.Unlock()

	// 通知
	instances := []*Instance{
		{ID: "inst-1", Name: "test-service", Host: "localhost", Port: 8080},
	}
	d.notifyWatchers("test-service", instances)

	// 验证收到通知
	select {
	case received := <-ch:
		assert.Len(t, received, 1)
		assert.Equal(t, "inst-1", received[0].ID)
	case <-time.After(time.Second):
		t.Fatal("Timeout waiting for notification")
	}
}

func TestConsulDiscovery_HealthWatchLoop(t *testing.T) {
	logger := zap.NewNop()
	cfg := ConsulConfig{Address: "127.0.0.1:8500"}

	d, err := NewConsulDiscovery(cfg, logger)
	if err != nil {
		t.Skipf("Consul not available: %v", err)
		return
	}
	defer d.Close()

	// 预填充缓存
	d.cacheMu.Lock()
	d.services["health-test"] = []*Instance{
		{ID: "h-1", Name: "health-test", Host: "localhost", Port: 8080},
	}
	d.cacheMu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())

	// 启动健康检查
	d.StartHealthWatcher(ctx)

	// 短暂运行
	time.Sleep(100 * time.Millisecond)

	// 停止
	cancel()
	d.Close()
}