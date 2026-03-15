package balancer

import (
	"math/rand"
	"sync"

	"github.com/mumble/mumble-next/proxy/internal/discovery"
)

// LeastConnectionsBalancer 最少连接负载均衡器
type LeastConnectionsBalancer struct {
	connections sync.Map // instanceID -> *int64
}

// NewLeastConnectionsBalancer 创建最少连接负载均衡器
func NewLeastConnectionsBalancer() *LeastConnectionsBalancer {
	return &LeastConnectionsBalancer{}
}

// Select 选择一个实例
func (b *LeastConnectionsBalancer) Select(instances []*discovery.Instance) (*discovery.Instance, error) {
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

	// 选择连接数最少的实例
	var selected *discovery.Instance
	minConns := int64(^uint64(0) >> 1) // MaxInt64

	for _, inst := range healthy {
		conns := b.getConnections(inst.ID)
		if conns < minConns {
			minConns = conns
			selected = inst
		}
	}

	// 增加连接计数
	if selected != nil {
		b.incrementConnections(selected.ID)
	}

	return selected, nil
}

// Name 返回负载均衡器名称
func (b *LeastConnectionsBalancer) Name() string {
	return "least_connections"
}

// Release 释放连接
func (b *LeastConnectionsBalancer) Release(instanceID string) {
	b.decrementConnections(instanceID)
}

// getConnections 获取连接数
func (b *LeastConnectionsBalancer) getConnections(instanceID string) int64 {
	val, ok := b.connections.Load(instanceID)
	if !ok {
		return 0
	}
	return *(val.(*int64))
}

// incrementConnections 增加连接数
func (b *LeastConnectionsBalancer) incrementConnections(instanceID string) {
	val, _ := b.connections.LoadOrStore(instanceID, new(int64))
	ptr := val.(*int64)
	(*ptr)++
}

// decrementConnections 减少连接数
func (b *LeastConnectionsBalancer) decrementConnections(instanceID string) {
	val, ok := b.connections.Load(instanceID)
	if !ok {
		return
	}
	ptr := val.(*int64)
	if *ptr > 0 {
		(*ptr)--
	}
}

// RandomBalancer 随机负载均衡器
type RandomBalancer struct{}

// NewRandomBalancer 创建随机负载均衡器
func NewRandomBalancer() *RandomBalancer {
	return &RandomBalancer{}
}

// Select 选择一个实例
func (b *RandomBalancer) Select(instances []*discovery.Instance) (*discovery.Instance, error) {
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

	idx := rand.Intn(len(healthy))
	return healthy[idx], nil
}

// Name 返回负载均衡器名称
func (b *RandomBalancer) Name() string {
	return "random"
}