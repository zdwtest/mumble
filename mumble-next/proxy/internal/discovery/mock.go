package discovery

import (
	"context"
	"errors"
	"sync"
	"time"
)

// MockDiscovery 用于测试的 Mock 服务发现
type MockDiscovery struct {
	instances map[string][]*Instance
	mu        sync.RWMutex
	watchers  map[string][]chan []*Instance
	watcherMu sync.Mutex
	closed    bool
	closeCh   chan struct{}

	// 用于测试的错误注入
	RegisterError   error
	DiscoverError   error
	RegisterCallCount int
	DiscoverCallCount int
}

// NewMockDiscovery 创建 Mock 服务发现
func NewMockDiscovery() *MockDiscovery {
	return &MockDiscovery{
		instances: make(map[string][]*Instance),
		watchers:  make(map[string][]chan []*Instance),
		closeCh:   make(chan struct{}),
	}
}

// Register 注册服务实例
func (m *MockDiscovery) Register(ctx context.Context, instance *Instance) error {
	m.RegisterCallCount++
	if m.RegisterError != nil {
		return m.RegisterError
	}

	m.mu.Lock()
	if instance.Weight == 0 {
		instance.Weight = 1
	}
	instance.UpdatedAt = time.Now()
	instance.Healthy = true

	m.instances[instance.Name] = append(m.instances[instance.Name], instance)
	instances := m.copyInstancesLocked(instance.Name)
	m.mu.Unlock()

	m.notifyWatchersUnlocked(instance.Name, instances)

	return nil
}

// copyInstancesLocked 复制实例列表 (调用者持有锁)
func (m *MockDiscovery) copyInstancesLocked(serviceName string) []*Instance {
	instances := m.instances[serviceName]
	cpy := make([]*Instance, len(instances))
	copy(cpy, instances)
	return cpy
}

// Deregister 注销服务实例
func (m *MockDiscovery) Deregister(ctx context.Context, instanceID string) error {
	m.mu.Lock()
	var instances []*Instance
	var serviceName string
	for name, insts := range m.instances {
		for i, inst := range insts {
			if inst.ID == instanceID {
				m.instances[name] = append(insts[:i], insts[i+1:]...)
				// 如果没有实例了，删除服务
				if len(m.instances[name]) == 0 {
					delete(m.instances, name)
				} else {
					instances = m.copyInstancesLocked(name)
				}
				serviceName = name
				m.mu.Unlock()
				if len(instances) > 0 {
					m.notifyWatchersUnlocked(serviceName, instances)
				}
				return nil
			}
		}
	}
	m.mu.Unlock()

	return nil
}

// Discover 发现服务实例
func (m *MockDiscovery) Discover(ctx context.Context, serviceName string) ([]*Instance, error) {
	m.DiscoverCallCount++
	if m.DiscoverError != nil {
		return nil, m.DiscoverError
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	instances, ok := m.instances[serviceName]
	if !ok {
		return nil, errors.New("service not found")
	}

	result := make([]*Instance, len(instances))
	copy(result, instances)
	return result, nil
}

// Watch 监听服务变更
func (m *MockDiscovery) Watch(ctx context.Context, serviceName string) (<-chan []*Instance, error) {
	ch := make(chan []*Instance, 10)

	m.watcherMu.Lock()
	// 检查是否已关闭
	if m.closed {
		m.watcherMu.Unlock()
		close(ch)
		return ch, nil
	}
	m.watchers[serviceName] = append(m.watchers[serviceName], ch)
	m.watcherMu.Unlock()

	// 立即发送当前实例
	go func() {
		defer func() {
			// 忽略向已关闭 channel 发送导致的 panic
			recover()
		}()

		m.mu.RLock()
		instances := m.instances[serviceName]
		cpy := make([]*Instance, len(instances))
		copy(cpy, instances)
		m.mu.RUnlock()

		if len(cpy) > 0 {
			select {
			case ch <- cpy:
			case <-m.closeCh:
			}
		}
	}()

	return ch, nil
}

// notifyWatchersUnlocked 通知观察者 (调用者不持有锁)
func (m *MockDiscovery) notifyWatchersUnlocked(serviceName string, instances []*Instance) {
	m.watcherMu.Lock()
	defer m.watcherMu.Unlock()

	// 如果已关闭，不发送
	if m.closed {
		return
	}

	for _, ch := range m.watchers[serviceName] {
		select {
		case ch <- instances:
		default:
		}
	}
}

// notifyWatchers 通知观察者
func (m *MockDiscovery) notifyWatchers(serviceName string) {
	m.mu.RLock()
	instances := m.copyInstancesLocked(serviceName)
	m.mu.RUnlock()

	m.notifyWatchersUnlocked(serviceName, instances)
}

// Close 关闭服务发现
func (m *MockDiscovery) Close() error {
	m.watcherMu.Lock()
	defer m.watcherMu.Unlock()

	if m.closed {
		return nil
	}

	m.closed = true
	close(m.closeCh)

	for _, watchers := range m.watchers {
		for _, ch := range watchers {
			close(ch)
		}
	}
	m.watchers = make(map[string][]chan []*Instance)

	return nil
}

// SetInstances 设置实例 (用于测试)
func (m *MockDiscovery) SetInstances(serviceName string, instances []*Instance) {
	m.mu.Lock()
	m.instances[serviceName] = instances
	cpy := m.copyInstancesLocked(serviceName)
	m.mu.Unlock()

	m.notifyWatchersUnlocked(serviceName, cpy)
}