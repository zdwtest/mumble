package balancer

import (
	"hash/crc32"
	"sort"
	"sync"

	"github.com/mumble/mumble-next/proxy/internal/discovery"
)

// ConsistentHashBalancer 一致性哈希负载均衡器
type ConsistentHashBalancer struct {
	virtualNodes int
	circle       []uint32
	nodeMap      map[uint32]*discovery.Instance
	mu           sync.RWMutex
}

// NewConsistentHashBalancer 创建一致性哈希负载均衡器
func NewConsistentHashBalancer(virtualNodes int) *ConsistentHashBalancer {
	if virtualNodes <= 0 {
		virtualNodes = 150
	}
	return &ConsistentHashBalancer{
		virtualNodes: virtualNodes,
		nodeMap:      make(map[uint32]*discovery.Instance),
	}
}

// Select 选择一个实例
func (b *ConsistentHashBalancer) Select(instances []*discovery.Instance) (*discovery.Instance, error) {
	return b.SelectWithKey(instances, "")
}

// SelectWithKey 根据键选择实例
func (b *ConsistentHashBalancer) SelectWithKey(instances []*discovery.Instance, key string) (*discovery.Instance, error) {
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

	// 重建哈希环
	b.rebuildCircle(healthy)

	// 计算键的哈希值
	var hash uint32
	if key != "" {
		hash = b.hashKey(key)
	} else {
		// 如果没有指定键，使用实例ID列表的哈希
		hash = b.hashKey(instances[0].ID)
	}

	b.mu.RLock()
	defer b.mu.RUnlock()

	// 在哈希环上查找
	idx := sort.Search(len(b.circle), func(i int) bool {
		return b.circle[i] >= hash
	})

	// 环形查找
	if idx >= len(b.circle) {
		idx = 0
	}

	return b.nodeMap[b.circle[idx]], nil
}

// Name 返回负载均衡器名称
func (b *ConsistentHashBalancer) Name() string {
	return "consistent_hash"
}

// rebuildCircle 重建哈希环
func (b *ConsistentHashBalancer) rebuildCircle(instances []*discovery.Instance) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.circle = make([]uint32, 0, len(instances)*b.virtualNodes)
	b.nodeMap = make(map[uint32]*discovery.Instance)

	for _, inst := range instances {
		for i := 0; i < b.virtualNodes; i++ {
			virtualKey := inst.ID + "#" + string(rune(i))
			hash := b.hashKey(virtualKey)
			b.circle = append(b.circle, hash)
			b.nodeMap[hash] = inst
		}
	}

	// 排序
	sort.Slice(b.circle, func(i, j int) bool {
		return b.circle[i] < b.circle[j]
	})
}

// hashKey 计算键的哈希值
func (b *ConsistentHashBalancer) hashKey(key string) uint32 {
	return crc32.ChecksumIEEE([]byte(key))
}

// GetCircle 获取哈希环 (用于调试)
func (b *ConsistentHashBalancer) GetCircle() []uint32 {
	b.mu.RLock()
	defer b.mu.RUnlock()

	result := make([]uint32, len(b.circle))
	copy(result, b.circle)
	return result
}