package balancer

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mumble/mumble-next/proxy/internal/discovery"
)

func createTestInstances(count int) []*discovery.Instance {
	instances := make([]*discovery.Instance, count)
	for i := 0; i < count; i++ {
		instances[i] = &discovery.Instance{
			ID:      string(rune('a' + i)),
			Name:    "test",
			Host:    "host",
			Port:    64738,
			Healthy: true,
			Weight:  1,
		}
	}
	return instances
}

func TestRoundRobinBalancer(t *testing.T) {
	b := NewRoundRobinBalancer()
	assert.Equal(t, "round_robin", b.Name())

	instances := createTestInstances(3)

	t.Run("RoundRobin", func(t *testing.T) {
		// 多次选择应该轮询
		results := make(map[string]int)
		for i := 0; i < 30; i++ {
			inst, err := b.Select(instances)
			require.NoError(t, err)
			results[inst.ID]++
		}

		// 每个实例应该被选中约10次
		for _, count := range results {
			assert.Equal(t, 10, count)
		}
	})

	t.Run("EmptyInstances", func(t *testing.T) {
		_, err := b.Select([]*discovery.Instance{})
		assert.Equal(t, ErrNoInstances, err)
	})

	t.Run("UnhealthyInstances", func(t *testing.T) {
		instances := createTestInstances(3)
		for _, inst := range instances {
			inst.Healthy = false
		}

		_, err := b.Select(instances)
		assert.Equal(t, ErrNoInstances, err)
	})

	t.Run("PartialHealthy", func(t *testing.T) {
		instances := createTestInstances(3)
		instances[0].Healthy = false
		instances[1].Healthy = true
		instances[2].Healthy = false

		// 应该只选择健康的实例
		for i := 0; i < 10; i++ {
			inst, err := b.Select(instances)
			require.NoError(t, err)
			assert.Equal(t, "b", inst.ID) // 只有第二个是健康的
		}
	})
}

func TestWeightedRoundRobinBalancer(t *testing.T) {
	b := NewWeightedRoundRobinBalancer()
	assert.Equal(t, "weighted_round_robin", b.Name())

	t.Run("WeightedDistribution", func(t *testing.T) {
		instances := []*discovery.Instance{
			{ID: "a", Name: "test", Host: "h1", Port: 64738, Healthy: true, Weight: 1},
			{ID: "b", Name: "test", Host: "h2", Port: 64738, Healthy: true, Weight: 2},
			{ID: "c", Name: "test", Host: "h3", Port: 64738, Healthy: true, Weight: 3},
		}

		results := make(map[string]int)
		for i := 0; i < 60; i++ {
			inst, err := b.Select(instances)
			require.NoError(t, err)
			results[inst.ID]++
		}

		// a 应该约10次, b 应该约20次, c 应该约30次
		assert.Equal(t, 10, results["a"])
		assert.Equal(t, 20, results["b"])
		assert.Equal(t, 30, results["c"])
	})

	t.Run("ZeroWeight", func(t *testing.T) {
		instances := []*discovery.Instance{
			{ID: "a", Name: "test", Host: "h1", Port: 64738, Healthy: true, Weight: 0},
		}

		inst, err := b.Select(instances)
		require.NoError(t, err)
		assert.Equal(t, "a", inst.ID)
	})

	t.Run("EmptyInstances", func(t *testing.T) {
		_, err := b.Select([]*discovery.Instance{})
		assert.Equal(t, ErrNoInstances, err)
	})
}

func TestLeastConnectionsBalancer(t *testing.T) {
	b := NewLeastConnectionsBalancer()
	assert.Equal(t, "least_connections", b.Name())

	t.Run("LeastConnections", func(t *testing.T) {
		instances := createTestInstances(3)

		// 第一次选择
		_, err := b.Select(instances)
		require.NoError(t, err)

		// 手动增加一些连接计数
		b.incrementConnections("a")
		b.incrementConnections("a")
		b.incrementConnections("b")

		// 应该选择连接数最少的 'c'
		inst2, err := b.Select(instances)
		require.NoError(t, err)
		assert.Equal(t, "c", inst2.ID)

		// 释放连接
		b.Release("a")
		b.Release("a")

		// 现在 'a' 应该有0个连接，应该被选中
		inst3, err := b.Select(instances)
		require.NoError(t, err)
		assert.Equal(t, "a", inst3.ID)
	})

	t.Run("EmptyInstances", func(t *testing.T) {
		_, err := b.Select([]*discovery.Instance{})
		assert.Equal(t, ErrNoInstances, err)
	})
}

func TestRandomBalancer(t *testing.T) {
	b := NewRandomBalancer()
	assert.Equal(t, "random", b.Name())

	instances := createTestInstances(3)

	t.Run("RandomSelection", func(t *testing.T) {
		results := make(map[string]int)
		for i := 0; i < 1000; i++ {
			inst, err := b.Select(instances)
			require.NoError(t, err)
			results[inst.ID]++
		}

		// 随机选择应该大致均匀分布 (每个约333次，允许偏差)
		for _, count := range results {
			assert.InDelta(t, 333, count, 100, "distribution should be roughly even")
		}
	})

	t.Run("EmptyInstances", func(t *testing.T) {
		_, err := b.Select([]*discovery.Instance{})
		assert.Equal(t, ErrNoInstances, err)
	})

	t.Run("UnhealthyInstances", func(t *testing.T) {
		instances := createTestInstances(3)
		for _, inst := range instances {
			inst.Healthy = false
		}

		_, err := b.Select(instances)
		assert.Equal(t, ErrNoInstances, err)
	})
}

func TestConsistentHashBalancer(t *testing.T) {
	b := NewConsistentHashBalancer(150)
	assert.Equal(t, "consistent_hash", b.Name())

	instances := createTestInstances(3)

	t.Run("ConsistentSelection", func(t *testing.T) {
		// 相同的键应该总是选择相同的实例
		key := "user-123"
		inst1, err := b.SelectWithKey(instances, key)
		require.NoError(t, err)

		for i := 0; i < 10; i++ {
			inst, err := b.SelectWithKey(instances, key)
			require.NoError(t, err)
			assert.Equal(t, inst1.ID, inst.ID, "same key should select same instance")
		}
	})

	t.Run("DifferentKeys", func(t *testing.T) {
		// 不同的键应该分布到不同的实例
		results := make(map[string]int)
		for i := 0; i < 100; i++ {
			key := string(rune(i))
			inst, err := b.SelectWithKey(instances, key)
			require.NoError(t, err)
			results[inst.ID]++
		}

		// 应该有多个实例被选中
		assert.GreaterOrEqual(t, len(results), 2)
	})

	t.Run("EmptyInstances", func(t *testing.T) {
		_, err := b.Select([]*discovery.Instance{})
		assert.Equal(t, ErrNoInstances, err)
	})

	t.Run("VirtualNodes", func(t *testing.T) {
		b := NewConsistentHashBalancer(0) // 测试默认值
		// 需要先选择实例来构建哈希环
		instances := createTestInstances(1)
		_, _ = b.Select(instances)
		circle := b.GetCircle()
		assert.NotEmpty(t, circle)
	})
}

func TestNewBalancer(t *testing.T) {
	tests := []struct {
		name     string
		cfg      Config
		expected string
	}{
		{"round_robin", Config{Type: RoundRobin}, "round_robin"},
		{"weighted_round_robin", Config{Type: WeightedRoundRobin}, "weighted_round_robin"},
		{"least_connections", Config{Type: LeastConnections}, "least_connections"},
		{"consistent_hash", Config{Type: ConsistentHash}, "consistent_hash"},
		{"random", Config{Type: Random}, "random"},
		{"unknown_defaults_to_rr", Config{Type: "unknown"}, "round_robin"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := NewBalancer(tt.cfg)
			assert.Equal(t, tt.expected, b.Name())
		})
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	assert.Equal(t, RoundRobin, cfg.Type)
	assert.Equal(t, 150, cfg.ConsistentHash.VirtualNodes)
}