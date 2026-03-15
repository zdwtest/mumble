package registry

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/mumble/mumble-next/proxy/internal/balancer"
	"github.com/mumble/mumble-next/proxy/internal/discovery"
)

func newTestRegistry(_ *testing.T) *Registry {
	d := discovery.NewMockDiscovery()
	b := balancer.NewRoundRobinBalancer()
	logger := zap.NewNop()

	return NewRegistry(d, b, logger)
}

func TestRegistry_Register(t *testing.T) {
	r := newTestRegistry(t)
	defer r.Close()

	inst := &discovery.Instance{
		ID:   "test-1",
		Name: "murmur",
		Host: "localhost",
		Port: 64738,
	}

	err := r.Register(context.Background(), inst)
	require.NoError(t, err)

	// 验证注册成功
	instances, err := r.Discover(context.Background(), "murmur")
	require.NoError(t, err)
	assert.Len(t, instances, 1)
	assert.Equal(t, "test-1", instances[0].ID)
}

func TestRegistry_Deregister(t *testing.T) {
	r := newTestRegistry(t)
	defer r.Close()

	inst := &discovery.Instance{
		ID:   "test-1",
		Name: "murmur",
		Host: "localhost",
		Port: 64738,
	}

	err := r.Register(context.Background(), inst)
	require.NoError(t, err)

	err = r.Deregister(context.Background(), "test-1")
	require.NoError(t, err)

	_, err = r.Discover(context.Background(), "murmur")
	assert.Error(t, err)
}

func TestRegistry_Select(t *testing.T) {
	r := newTestRegistry(t)
	defer r.Close()

	// 注册多个实例
	for i := 0; i < 3; i++ {
		inst := &discovery.Instance{
			ID:      string(rune('a' + i)),
			Name:    "murmur",
			Host:    "localhost",
			Port:    64738,
			Healthy: true,
		}
		err := r.Register(context.Background(), inst)
		require.NoError(t, err)
	}

	t.Run("Select", func(t *testing.T) {
		inst, err := r.Select(context.Background(), "murmur")
		require.NoError(t, err)
		assert.NotEmpty(t, inst.ID)
	})

	t.Run("SelectNotFound", func(t *testing.T) {
		_, err := r.Select(context.Background(), "unknown")
		assert.Error(t, err)
	})

	t.Run("SelectUnhealthy", func(t *testing.T) {
		// 设置所有实例为不健康
		instances, _ := r.Discover(context.Background(), "murmur")
		for _, inst := range instances {
			inst.Healthy = false
		}

		_, err := r.Select(context.Background(), "murmur")
		assert.Equal(t, ErrNoHealthyInstance, err)
	})
}

func TestRegistry_SelectWithKey(t *testing.T) {
	// 使用一致性哈希负载均衡器
	d := discovery.NewMockDiscovery()
	b := balancer.NewConsistentHashBalancer(150)
	logger := zap.NewNop()
	r := NewRegistry(d, b, logger)
	defer r.Close()

	// 注册实例
	for i := 0; i < 3; i++ {
		inst := &discovery.Instance{
			ID:      string(rune('a' + i)),
			Name:    "murmur",
			Host:    "localhost",
			Port:    64738,
			Healthy: true,
		}
		err := r.Register(context.Background(), inst)
		require.NoError(t, err)
	}

	t.Run("SameKeySameInstance", func(t *testing.T) {
		key := "user-123"

		inst1, err := r.SelectWithKey(context.Background(), "murmur", key)
		require.NoError(t, err)

		for i := 0; i < 10; i++ {
			inst, err := r.SelectWithKey(context.Background(), "murmur", key)
			require.NoError(t, err)
			assert.Equal(t, inst1.ID, inst.ID)
		}
	})

	t.Run("DifferentKeys", func(t *testing.T) {
		results := make(map[string]int)
		for i := 0; i < 30; i++ {
			key := string(rune(i))
			inst, err := r.SelectWithKey(context.Background(), "murmur", key)
			require.NoError(t, err)
			results[inst.ID]++
		}

		// 应该分布到多个实例
		assert.GreaterOrEqual(t, len(results), 2)
	})
}

func TestRegistry_Cache(t *testing.T) {
	r := newTestRegistry(t)
	defer r.Close()

	inst := &discovery.Instance{
		ID:      "test-1",
		Name:    "murmur",
		Host:    "localhost",
		Port:    64738,
		Healthy: true,
	}
	err := r.Register(context.Background(), inst)
	require.NoError(t, err)

	// 首次选择会缓存
	_, err = r.Select(context.Background(), "murmur")
	require.NoError(t, err)

	// 验证缓存存在
	r.cacheMu.RLock()
	cached, ok := r.cache["murmur"]
	r.cacheMu.RUnlock()

	assert.True(t, ok)
	assert.Len(t, cached, 1)

	// 使缓存失效
	r.InvalidateCache("murmur")

	r.cacheMu.RLock()
	_, ok = r.cache["murmur"]
	r.cacheMu.RUnlock()

	assert.False(t, ok)
}

func TestRegistry_Watch(t *testing.T) {
	r := newTestRegistry(t)
	defer r.Close()

	ch, err := r.Watch(context.Background(), "murmur")
	require.NoError(t, err)

	// 注册实例
	inst := &discovery.Instance{
		ID:      "test-1",
		Name:    "murmur",
		Host:    "localhost",
		Port:    64738,
		Healthy: true,
	}
	err = r.Register(context.Background(), inst)
	require.NoError(t, err)

	// 等待通知
	select {
	case instances := <-ch:
		assert.Len(t, instances, 1)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for watch notification")
	}
}

func TestRegistry_Getters(t *testing.T) {
	r := newTestRegistry(t)
	defer r.Close()

	assert.NotNil(t, r.GetDiscovery())
	assert.NotNil(t, r.GetBalancer())
}

func TestHealthChecker(t *testing.T) {
	d := discovery.NewMockDiscovery()
	logger := zap.NewNop()
	h := NewHealthChecker(d, logger)

	inst := &discovery.Instance{
		ID:      "test-1",
		Name:    "murmur",
		Host:    "localhost",
		Port:    64738,
		Healthy: true,
	}

	t.Run("AddRemove", func(t *testing.T) {
		h.AddInstance(inst)

		h.mu.RLock()
		_, ok := h.instances["test-1"]
		h.mu.RUnlock()
		assert.True(t, ok)

		h.RemoveInstance("test-1")

		h.mu.RLock()
		_, ok = h.instances["test-1"]
		h.mu.RUnlock()
		assert.False(t, ok)
	})

	t.Run("StartStop", func(t *testing.T) {
		h.Start(time.Second)
		assert.True(t, h.running)

		h.Stop()
		assert.False(t, h.running)

		// 重复调用应该安全
		h.Stop()
		h.Stop()
	})
}

func TestRegistryHealthCheck(t *testing.T) {
	d := discovery.NewMockDiscovery()
	b := balancer.NewRoundRobinBalancer()
	logger := zap.NewNop()
	r := NewRegistry(d, b, logger)
	defer r.Close()

	// 启动健康检查
	r.StartHealthCheck(time.Second)
	time.Sleep(50 * time.Millisecond)

	// 停止健康检查
	r.StopHealthCheck()
}

func TestRegistryCloseTwice(t *testing.T) {
	d := discovery.NewMockDiscovery()
	b := balancer.NewRoundRobinBalancer()
	logger := zap.NewNop()
	r := NewRegistry(d, b, logger)

	// 第一次关闭
	err := r.Close()
	require.NoError(t, err)

	// 第二次关闭应该安全
	err = r.Close()
	require.NoError(t, err)
}

func TestRegistrySelectWithNonConsistentHash(t *testing.T) {
	d := discovery.NewMockDiscovery()
	b := balancer.NewRoundRobinBalancer() // 不支持 SelectWithKey
	logger := zap.NewNop()
	r := NewRegistry(d, b, logger)
	defer r.Close()

	// 注册实例
	inst := &discovery.Instance{
		ID:      "test-1",
		Name:    "murmur",
		Host:    "localhost",
		Port:    64738,
		Healthy: true,
	}
	err := r.Register(context.Background(), inst)
	require.NoError(t, err)

	// 应该回退到普通 Select
	selected, err := r.SelectWithKey(context.Background(), "murmur", "user-123")
	require.NoError(t, err)
	assert.Equal(t, "test-1", selected.ID)
}

func TestHealthCheckerCheckAll(t *testing.T) {
	d := discovery.NewMockDiscovery()
	logger := zap.NewNop()
	h := NewHealthChecker(d, logger)

	// 启动一个简单的 TCP 监听器
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	port := listener.Addr().(*net.TCPAddr).Port

	// 添加实例
	inst := &discovery.Instance{
		ID:      "test-1",
		Name:    "murmur",
		Host:    "127.0.0.1",
		Port:    port,
		Healthy: false, // 初始为不健康
	}
	h.AddInstance(inst)

	// 运行 checkAll
	h.checkAll()

	// 验证实例被标记为健康
	h.mu.RLock()
	checkedInst := h.instances["test-1"]
	h.mu.RUnlock()

	assert.True(t, checkedInst.Healthy)
}

func TestHealthCheckerCheckInstance(t *testing.T) {
	d := discovery.NewMockDiscovery()
	logger := zap.NewNop()
	h := NewHealthChecker(d, logger)

	t.Run("ConnectionSuccess", func(t *testing.T) {
		// 启动监听器
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)
		defer listener.Close()

		port := listener.Addr().(*net.TCPAddr).Port
		inst := &discovery.Instance{
			ID:   "test-1",
			Host: "127.0.0.1",
			Port: port,
		}

		result := h.checkInstance(inst)
		assert.True(t, result)
	})

	t.Run("ConnectionFailed", func(t *testing.T) {
		inst := &discovery.Instance{
			ID:   "test-2",
			Host: "127.0.0.1",
			Port: 59999, // 不存在的端口
		}

		result := h.checkInstance(inst)
		assert.False(t, result)
	})
}

func TestHealthCheckerCheckMethod(t *testing.T) {
	d := discovery.NewMockDiscovery()
	logger := zap.NewNop()
	h := NewHealthChecker(d, logger)

	t.Run("Success", func(t *testing.T) {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)
		defer listener.Close()

		port := listener.Addr().(*net.TCPAddr).Port
		inst := &discovery.Instance{
			ID:   "test-1",
			Host: "127.0.0.1",
			Port: port,
		}

		err = h.Check(inst)
		assert.NoError(t, err)
	})

	t.Run("Failure", func(t *testing.T) {
		inst := &discovery.Instance{
			ID:   "test-2",
			Host: "127.0.0.1",
			Port: 59999,
		}

		err := h.Check(inst)
		assert.Error(t, err)
	})
}

func TestHealthCheckerRunLoop(t *testing.T) {
	d := discovery.NewMockDiscovery()
	logger := zap.NewNop()
	h := NewHealthChecker(d, logger)

	// 添加实例
	inst := &discovery.Instance{
		ID:   "test-1",
		Name: "murmur",
		Host: "127.0.0.1",
		Port: 59999, // 不存在的端口
	}
	h.AddInstance(inst)

	// 启动健康检查
	h.Start(100 * time.Millisecond)

	// 等待几轮检查
	time.Sleep(250 * time.Millisecond)

	// 停止
	h.Stop()
	assert.False(t, h.running)
}