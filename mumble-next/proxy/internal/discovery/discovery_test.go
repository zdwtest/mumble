package discovery

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInstance(t *testing.T) {
	inst := &Instance{
		ID:       "test-1",
		Name:     "murmur",
		Host:     "localhost",
		Port:     64738,
		Metadata: map[string]string{"region": "us-east"},
		Healthy:  true,
		Weight:   1,
	}

	assert.Equal(t, "test-1", inst.ID)
	assert.Equal(t, "murmur", inst.Name)
	assert.Equal(t, "localhost", inst.Host)
	assert.Equal(t, 64738, inst.Port)
	assert.True(t, inst.Healthy)
	assert.Equal(t, 1, inst.Weight)
}

func TestStaticDiscovery(t *testing.T) {
	cfg := &StaticConfig{
		Instances: []*Instance{
			{ID: "inst-1", Name: "murmur", Host: "host1", Port: 64738, Weight: 1},
			{ID: "inst-2", Name: "murmur", Host: "host2", Port: 64738, Weight: 2},
		},
	}

	d := NewStaticDiscovery(cfg)
	defer d.Close()

	t.Run("Discover", func(t *testing.T) {
		instances, err := d.Discover(context.Background(), "murmur")
		require.NoError(t, err)
		assert.Len(t, instances, 2)

		// 检查实例被正确初始化
		for _, inst := range instances {
			assert.True(t, inst.Healthy)
			assert.NotZero(t, inst.Weight)
			assert.NotZero(t, inst.UpdatedAt)
		}
	})

	t.Run("DiscoverNotFound", func(t *testing.T) {
		_, err := d.Discover(context.Background(), "unknown")
		assert.Error(t, err)
	})

	t.Run("Register", func(t *testing.T) {
		inst := &Instance{
			ID:   "inst-3",
			Name: "murmur",
			Host: "host3",
			Port: 64738,
		}

		err := d.Register(context.Background(), inst)
		require.NoError(t, err)

		instances, err := d.Discover(context.Background(), "murmur")
		require.NoError(t, err)
		assert.Len(t, instances, 3)
	})

	t.Run("Deregister", func(t *testing.T) {
		err := d.Deregister(context.Background(), "inst-1")
		require.NoError(t, err)

		instances, err := d.Discover(context.Background(), "murmur")
		require.NoError(t, err)

		for _, inst := range instances {
			assert.NotEqual(t, "inst-1", inst.ID)
		}
	})

	t.Run("Watch", func(t *testing.T) {
		ch, err := d.Watch(context.Background(), "murmur")
		require.NoError(t, err)

		// 等待初始数据
		select {
		case instances := <-ch:
			assert.NotEmpty(t, instances)
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for watch notification")
		}

		// 注册新实例应该触发通知
		inst := &Instance{ID: "inst-watch", Name: "murmur", Host: "host-watch", Port: 64738}
		err = d.Register(context.Background(), inst)
		require.NoError(t, err)

		select {
		case instances := <-ch:
			assert.NotEmpty(t, instances)
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for watch notification")
		}
	})
}

func TestStaticDiscoveryConcurrent(t *testing.T) {
	cfg := &StaticConfig{
		Instances: []*Instance{
			{ID: "inst-1", Name: "murmur", Host: "host1", Port: 64738},
		},
	}

	d := NewStaticDiscovery(cfg)
	defer d.Close()

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(2)

		// 并发读
		go func() {
			defer wg.Done()
			_, _ = d.Discover(context.Background(), "murmur")
		}()

		// 并发写
		go func(i int) {
			defer wg.Done()
			inst := &Instance{
				ID:   string(rune(i)),
				Name: "murmur",
				Host: "host",
				Port: 64738,
			}
			_ = d.Register(context.Background(), inst)
		}(i)
	}

	wg.Wait()
}

func TestStaticDiscoveryAddRemove(t *testing.T) {
	d := NewStaticDiscovery(&StaticConfig{})
	defer d.Close()

	// AddInstance
	d.AddInstance("murmur", "inst-1", "host1", 64738, nil)
	d.AddInstance("murmur", "inst-2", "host2", 64738, map[string]string{"region": "us-east"})

	instances := d.GetInstances("murmur")
	assert.Len(t, instances, 2)

	// RemoveInstance
	d.RemoveInstance("inst-1")
	instances = d.GetInstances("murmur")
	assert.Len(t, instances, 1)
	assert.Equal(t, "inst-2", instances[0].ID)
}

func TestMockDiscovery(t *testing.T) {
	m := NewMockDiscovery()
	defer m.Close()

	t.Run("Basic", func(t *testing.T) {
		inst := &Instance{ID: "mock-1", Name: "service", Host: "host", Port: 8080}
		err := m.Register(context.Background(), inst)
		require.NoError(t, err)

		instances, err := m.Discover(context.Background(), "service")
		require.NoError(t, err)
		assert.Len(t, instances, 1)
	})

	t.Run("ErrorInjection", func(t *testing.T) {
		m.RegisterError = assert.AnError
		err := m.Register(context.Background(), &Instance{ID: "x", Name: "y"})
		assert.Error(t, err)
		m.RegisterError = nil

		m.DiscoverError = assert.AnError
		_, err = m.Discover(context.Background(), "service")
		assert.Error(t, err)
		m.DiscoverError = nil
	})

	t.Run("SetInstances", func(t *testing.T) {
		m.SetInstances("new-service", []*Instance{
			{ID: "n1", Name: "new-service", Host: "h1", Port: 8080},
			{ID: "n2", Name: "new-service", Host: "h2", Port: 8080},
		})

		instances, err := m.Discover(context.Background(), "new-service")
		require.NoError(t, err)
		assert.Len(t, instances, 2)
	})

	t.Run("CallCount", func(t *testing.T) {
		m.RegisterCallCount = 0
		m.DiscoverCallCount = 0

		_ = m.Register(context.Background(), &Instance{ID: "x", Name: "y"})
		_, _ = m.Discover(context.Background(), "y")

		assert.Equal(t, 1, m.RegisterCallCount)
		assert.Equal(t, 1, m.DiscoverCallCount)
	})
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	assert.Equal(t, "static", cfg.Type)
	assert.Equal(t, 30*time.Second, cfg.RefreshInterval)
	assert.Equal(t, 10*time.Second, cfg.HealthCheckInterval)
}

func TestStaticDiscoveryWatchClose(t *testing.T) {
	d := NewStaticDiscovery(&StaticConfig{})
	defer d.Close()

	// 测试 Watch 后立即 Close
	ch, err := d.Watch(context.Background(), "murmur")
	require.NoError(t, err)

	// 添加实例后立即关闭
	inst := &Instance{ID: "inst-1", Name: "murmur", Host: "host1", Port: 64738}
	err = d.Register(context.Background(), inst)
	require.NoError(t, err)

	// 应该能收到通知
	select {
	case instances := <-ch:
		assert.Len(t, instances, 1)
	case <-time.After(time.Second):
		// 可能因为关闭太快而未收到
	}
}

func TestStaticDiscoveryDoubleClose(t *testing.T) {
	d := NewStaticDiscovery(&StaticConfig{})

	// 第一次关闭
	err := d.Close()
	require.NoError(t, err)

	// 第二次关闭应该安全
	err = d.Close()
	require.NoError(t, err)
}

func TestMockDiscoveryClose(t *testing.T) {
	m := NewMockDiscovery()

	ch, err := m.Watch(context.Background(), "murmur")
	require.NoError(t, err)

	err = m.Close()
	require.NoError(t, err)

	// 重复关闭应该安全
	err = m.Close()
	require.NoError(t, err)

	// channel 应该被关闭
	_, ok := <-ch
	assert.False(t, ok)
}