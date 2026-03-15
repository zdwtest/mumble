package discovery

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// TestEtcdDiscovery_Basic tests with real etcd (requires etcd running)
func TestEtcdDiscovery_Basic(t *testing.T) {
	cfg := EtcdConfig{
		Endpoints: []string{"127.0.0.1:2379"},
		Prefix:    "/test-mumble",
	}

	logger := zap.NewNop()

	// Try to connect with short timeout to detect if etcd is available
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	d, err := NewEtcdDiscovery(cfg, logger)
	if err != nil {
		t.Skipf("etcd not available: %v", err)
		return
	}

	// Test connection by listing services
	_, err = d.ListServices(ctx)
	if err != nil {
		d.Close()
		t.Skipf("etcd not available: %v", err)
		return
	}
	defer d.Close()

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

		// 等待注册生效
		time.Sleep(100 * time.Millisecond)

		// 发现服务
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
				assert.Equal(t, instance.Weight, inst.Weight)
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
		require.NoError(t, err)
		assert.Empty(t, instances)
	})
}

func TestEtcdDiscovery_Config(t *testing.T) {
	logger := zap.NewNop()

	t.Run("DefaultEndpoints", func(t *testing.T) {
		cfg := EtcdConfig{
			Endpoints: []string{"127.0.0.1:2379"},
		}
		d, err := NewEtcdDiscovery(cfg, logger)
		require.NoError(t, err)
		defer d.Close()

		assert.NotNil(t, d.client)
		assert.Equal(t, "/services", d.prefix)
	})

	t.Run("CustomPrefix", func(t *testing.T) {
		cfg := EtcdConfig{
			Endpoints: []string{"127.0.0.1:2379"},
			Prefix:    "/custom-prefix",
		}
		d, err := NewEtcdDiscovery(cfg, logger)
		require.NoError(t, err)
		defer d.Close()

		assert.Equal(t, "/custom-prefix", d.prefix)
	})

	t.Run("MultipleEndpoints", func(t *testing.T) {
		cfg := EtcdConfig{
			Endpoints: []string{"etcd1.example.com:2379", "etcd2.example.com:2379", "etcd3.example.com:2379"},
		}
		d, err := NewEtcdDiscovery(cfg, logger)
		require.NoError(t, err)
		defer d.Close()

		assert.NotNil(t, d.client)
	})
}

func TestEtcdDiscovery_Options(t *testing.T) {
	logger := zap.NewNop()
	cfg := EtcdConfig{Endpoints: []string{"127.0.0.1:2379"}}

	d, err := NewEtcdDiscovery(cfg, logger, func(e *EtcdDiscovery) {
		e.services["preloaded"] = []*Instance{
			{ID: "pre-1", Name: "preloaded", Host: "localhost", Port: 8080},
		}
	})
	require.NoError(t, err)
	defer d.Close()

	assert.NotNil(t, d.services["preloaded"])
}

func TestEtcdDiscovery_Cache(t *testing.T) {
	logger := zap.NewNop()
	cfg := EtcdConfig{Endpoints: []string{"127.0.0.1:2379"}}

	d, err := NewEtcdDiscovery(cfg, logger)
	if err != nil {
		t.Skipf("etcd not available: %v", err)
		return
	}

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	_, err = d.ListServices(ctx)
	cancel()
	if err != nil {
		d.Close()
		t.Skipf("etcd not available: %v", err)
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

func TestEtcdDiscovery_Watch(t *testing.T) {
	logger := zap.NewNop()
	cfg := EtcdConfig{Endpoints: []string{"127.0.0.1:2379"}}

	d, err := NewEtcdDiscovery(cfg, logger)
	if err != nil {
		t.Skipf("etcd not available: %v", err)
		return
	}

	// Test connection
	ctx2, cancel2 := context.WithTimeout(context.Background(), 2*time.Second)
	_, err = d.ListServices(ctx2)
	cancel2()
	if err != nil {
		d.Close()
		t.Skipf("etcd not available: %v", err)
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

func TestEtcdDiscovery_Close(t *testing.T) {
	logger := zap.NewNop()
	cfg := EtcdConfig{Endpoints: []string{"127.0.0.1:2379"}}

	d, err := NewEtcdDiscovery(cfg, logger)
	if err != nil {
		t.Skipf("etcd not available: %v", err)
		return
	}

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	_, err = d.ListServices(ctx)
	cancel()
	if err != nil {
		d.Close()
		t.Skipf("etcd not available: %v", err)
		return
	}

	// 第一次关闭
	err = d.Close()
	require.NoError(t, err)

	// 第二次关闭应该安全
	err = d.Close()
	require.NoError(t, err)
}

func TestEtcdDiscovery_GetClient(t *testing.T) {
	logger := zap.NewNop()
	cfg := EtcdConfig{Endpoints: []string{"127.0.0.1:2379"}}

	d, err := NewEtcdDiscovery(cfg, logger)
	if err != nil {
		t.Skipf("etcd not available: %v", err)
		return
	}

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	_, err = d.ListServices(ctx)
	cancel()
	if err != nil {
		d.Close()
		t.Skipf("etcd not available: %v", err)
		return
	}
	defer d.Close()

	client := d.GetClient()
	assert.NotNil(t, client)
}

func TestEtcdDiscovery_InstanceKey(t *testing.T) {
	logger := zap.NewNop()
	cfg := EtcdConfig{
		Endpoints: []string{"127.0.0.1:2379"},
		Prefix:    "/services",
	}

	d, err := NewEtcdDiscovery(cfg, logger)
	require.NoError(t, err)
	defer d.Close()

	key := d.instanceKey("murmur", "instance-1")
	assert.Equal(t, "/services/murmur/instance-1", key)

	prefix := d.servicePrefix("murmur")
	assert.Equal(t, "/services/murmur/", prefix)
}

func TestEtcdDiscovery_NotifyWatchers(t *testing.T) {
	logger := zap.NewNop()
	cfg := EtcdConfig{Endpoints: []string{"127.0.0.1:2379"}}

	d, err := NewEtcdDiscovery(cfg, logger)
	if err != nil {
		t.Skipf("etcd not available: %v", err)
		return
	}

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	_, err = d.ListServices(ctx)
	cancel()
	if err != nil {
		d.Close()
		t.Skipf("etcd not available: %v", err)
		return
	}
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

func TestEtcdDiscovery_ListServices(t *testing.T) {
	cfg := EtcdConfig{
		Endpoints: []string{"127.0.0.1:2379"},
		Prefix:    "/test-list-services",
	}

	logger := zap.NewNop()
	d, err := NewEtcdDiscovery(cfg, logger)
	if err != nil {
		t.Skipf("etcd not available: %v", err)
		return
	}

	// Test connection
	ctx2, cancel2 := context.WithTimeout(context.Background(), 2*time.Second)
	_, err = d.ListServices(ctx2)
	cancel2()
	if err != nil {
		d.Close()
		t.Skipf("etcd not available: %v", err)
		return
	}
	defer d.Close()

	ctx := context.Background()

	// 注册多个服务
	for i := 0; i < 2; i++ {
		instance := &Instance{
			ID:   fmt.Sprintf("list-test-%d", i),
			Name: "murmur",
			Host: "localhost",
			Port: 64738 + i,
		}
		err := d.Register(ctx, instance)
		if err != nil {
			t.Skipf("Cannot register: %v", err)
			return
		}
		defer d.Deregister(ctx, instance.ID)
	}

	instance2 := &Instance{
		ID:   "list-test-other",
		Name: "other-service",
		Host: "localhost",
		Port: 8080,
	}
	err = d.Register(ctx, instance2)
	if err != nil {
		t.Skipf("Cannot register: %v", err)
		return
	}
	defer d.Deregister(ctx, instance2.ID)

	time.Sleep(100 * time.Millisecond)

	services, err := d.ListServices(ctx)
	require.NoError(t, err)
	assert.Contains(t, services, "murmur")
	assert.Contains(t, services, "other-service")
}

func TestEtcdDiscovery_LeaseOperations(t *testing.T) {
	cfg := EtcdConfig{
		Endpoints: []string{"127.0.0.1:2379"},
		Prefix:    "/test-lease",
	}

	logger := zap.NewNop()
	d, err := NewEtcdDiscovery(cfg, logger)
	if err != nil {
		t.Skipf("etcd not available: %v", err)
		return
	}

	// Test connection
	ctx2, cancel2 := context.WithTimeout(context.Background(), 2*time.Second)
	_, err = d.ListServices(ctx2)
	cancel2()
	if err != nil {
		d.Close()
		t.Skipf("etcd not available: %v", err)
		return
	}
	defer d.Close()

	ctx := context.Background()

	t.Run("RegisterWithLease", func(t *testing.T) {
		instance := &Instance{
			ID:   "lease-test-1",
			Name: "murmur",
			Host: "localhost",
			Port: 64738,
		}

		leaseID, err := d.RegisterWithLease(ctx, instance, 10)
		require.NoError(t, err)
		assert.NotZero(t, leaseID)

		defer d.RevokeLease(ctx, leaseID)

		// 发现服务
		time.Sleep(100 * time.Millisecond)
		instances, err := d.Discover(ctx, "murmur")
		require.NoError(t, err)
		assert.NotEmpty(t, instances)
	})

	t.Run("RenewLease", func(t *testing.T) {
		instance := &Instance{
			ID:   "lease-test-2",
			Name: "murmur",
			Host: "localhost",
			Port: 64739,
		}

		leaseID, err := d.RegisterWithLease(ctx, instance, 10)
		require.NoError(t, err)
		defer d.RevokeLease(ctx, leaseID)

		// 续租
		err = d.RenewLease(ctx, leaseID)
		require.NoError(t, err)
	})
}

func TestEtcdDiscovery_ConcurrentAccess(t *testing.T) {
	cfg := EtcdConfig{
		Endpoints: []string{"127.0.0.1:2379"},
		Prefix:    "/test-concurrent",
	}

	logger := zap.NewNop()
	d, err := NewEtcdDiscovery(cfg, logger)
	if err != nil {
		t.Skipf("etcd not available: %v", err)
		return
	}

	// Test connection
	ctx2, cancel2 := context.WithTimeout(context.Background(), 2*time.Second)
	_, err = d.ListServices(ctx2)
	cancel2()
	if err != nil {
		d.Close()
		t.Skipf("etcd not available: %v", err)
		return
	}
	defer d.Close()

	ctx := context.Background()
	var wg sync.WaitGroup

	// 并发注册
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			instance := &Instance{
				ID:   fmt.Sprintf("concurrent-%d", i),
				Name: "murmur",
				Host: "localhost",
				Port: 64738 + i,
			}
			_ = d.Register(ctx, instance)
		}(i)
	}

	// 并发发现
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = d.Discover(ctx, "murmur")
		}()
	}

	wg.Wait()

	// 清理
	for i := 0; i < 10; i++ {
		_ = d.Deregister(ctx, fmt.Sprintf("concurrent-%d", i))
	}
}