package balancer

import (
	"sync/atomic"

	"github.com/mumble/mumble-next/proxy/internal/discovery"
)

// RoundRobinBalancer 轮询负载均衡器
type RoundRobinBalancer struct {
	counter atomic.Uint64
}

// NewRoundRobinBalancer 创建轮询负载均衡器
func NewRoundRobinBalancer() *RoundRobinBalancer {
	return &RoundRobinBalancer{}
}

// Select 选择一个实例
func (b *RoundRobinBalancer) Select(instances []*discovery.Instance) (*discovery.Instance, error) {
	if len(instances) == 0 {
		return nil, ErrNoInstances
	}

	// 过滤健康实例
	healthy := make([]*discovery.Instance, 0, len(instances))
	for _, inst := range instances {
		if inst.Healthy {
			healthy = append(healthy, inst)
		}
	}

	if len(healthy) == 0 {
		return nil, ErrNoInstances
	}

	idx := b.counter.Add(1) - 1
	return healthy[idx%uint64(len(healthy))], nil
}

// Name 返回负载均衡器名称
func (b *RoundRobinBalancer) Name() string {
	return "round_robin"
}

// WeightedRoundRobinBalancer 加权轮询负载均衡器
type WeightedRoundRobinBalancer struct {
	counter atomic.Uint64
}

// NewWeightedRoundRobinBalancer 创建加权轮询负载均衡器
func NewWeightedRoundRobinBalancer() *WeightedRoundRobinBalancer {
	return &WeightedRoundRobinBalancer{}
}

// Select 选择一个实例
func (b *WeightedRoundRobinBalancer) Select(instances []*discovery.Instance) (*discovery.Instance, error) {
	if len(instances) == 0 {
		return nil, ErrNoInstances
	}

	// 过滤健康实例并计算权重
	healthy := make([]*discovery.Instance, 0, len(instances))
	totalWeight := 0
	for _, inst := range instances {
		if inst.Healthy {
			if inst.Weight <= 0 {
				inst.Weight = 1
			}
			healthy = append(healthy, inst)
			totalWeight += inst.Weight
		}
	}

	if len(healthy) == 0 {
		return nil, ErrNoInstances
	}

	// 加权选择
	idx := b.counter.Add(1) - 1
	pos := idx % uint64(totalWeight)

	for _, inst := range healthy {
		if pos < uint64(inst.Weight) {
			return inst, nil
		}
		pos -= uint64(inst.Weight)
	}

	return healthy[0], nil
}

// Name 返回负载均衡器名称
func (b *WeightedRoundRobinBalancer) Name() string {
	return "weighted_round_robin"
}