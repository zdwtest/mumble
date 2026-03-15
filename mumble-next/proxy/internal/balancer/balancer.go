// Package balancer 提供负载均衡接口和实现
package balancer

import (
	"errors"

	"github.com/mumble/mumble-next/proxy/internal/discovery"
)

// ErrNoInstances 没有可用实例错误
var ErrNoInstances = errors.New("no instances available")

// Balancer 负载均衡接口
type Balancer interface {
	// Select 选择一个实例
	Select(instances []*discovery.Instance) (*discovery.Instance, error)

	// Name 返回负载均衡器名称
	Name() string
}

// Type 负载均衡类型
type Type string

const (
	// RoundRobin 轮询
	RoundRobin Type = "round_robin"
	// WeightedRoundRobin 加权轮询
	WeightedRoundRobin Type = "weighted_round_robin"
	// LeastConnections 最少连接
	LeastConnections Type = "least_connections"
	// ConsistentHash 一致性哈希
	ConsistentHash Type = "consistent_hash"
	// Random 随机
	Random Type = "random"
)

// Config 负载均衡配置
type Config struct {
	// Type 负载均衡类型
	Type Type `mapstructure:"type"`

	// ConsistentHash 一致性哈希配置
	ConsistentHash ConsistentHashConfig `mapstructure:"consistent_hash"`
}

// ConsistentHashConfig 一致性哈希配置
type ConsistentHashConfig struct {
	// VirtualNodes 虚拟节点数
	VirtualNodes int `mapstructure:"virtual_nodes"`
}

// DefaultConfig 返回默认配置
func DefaultConfig() Config {
	return Config{
		Type: RoundRobin,
		ConsistentHash: ConsistentHashConfig{
			VirtualNodes: 150,
		},
	}
}

// NewBalancer 根据类型创建负载均衡器
func NewBalancer(cfg Config) Balancer {
	switch cfg.Type {
	case RoundRobin:
		return NewRoundRobinBalancer()
	case WeightedRoundRobin:
		return NewWeightedRoundRobinBalancer()
	case LeastConnections:
		return NewLeastConnectionsBalancer()
	case ConsistentHash:
		return NewConsistentHashBalancer(cfg.ConsistentHash.VirtualNodes)
	case Random:
		return NewRandomBalancer()
	default:
		return NewRoundRobinBalancer()
	}
}