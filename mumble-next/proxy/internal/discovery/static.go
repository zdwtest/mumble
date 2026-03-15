package discovery

import (
	"context"
	"errors"
	"sync"
	"time"
)

// StaticDiscovery 静态服务发现实现
type StaticDiscovery struct {
	instances map[string][]*Instance // serviceName -> instances
	mu        sync.RWMutex
	watchers  map[string][]chan []*Instance
	watcherMu sync.Mutex
	closed    bool
	closeCh   chan struct{}
}

// NewStaticDiscovery 创建静态服务发现
func NewStaticDiscovery(cfg *StaticConfig) *StaticDiscovery {
	d := &StaticDiscovery{
		instances: make(map[string][]*Instance),
		watchers:  make(map[string][]chan []*Instance),
		closeCh:   make(chan struct{}),
	}

	// 加载静态实例
	for _, inst := range cfg.Instances {
		if inst.Weight == 0 {
			inst.Weight = 1
		}
		inst.UpdatedAt = time.Now()
		inst.Healthy = true
		d.instances[inst.Name] = append(d.instances[inst.Name], inst)
	}

	return d
}

// Register 注册服务实例
func (d *StaticDiscovery) Register(ctx context.Context, instance *Instance) error {
	d.mu.Lock()
	if instance.Weight == 0 {
		instance.Weight = 1
	}
	instance.UpdatedAt = time.Now()
	instance.Healthy = true

	d.instances[instance.Name] = append(d.instances[instance.Name], instance)
	instances := d.copyInstancesLocked(instance.Name)
	d.mu.Unlock()

	d.notifyWatchersUnlocked(instance.Name, instances)

	return nil
}

// copyInstancesLocked 复制实例列表 (调用者持有锁)
func (d *StaticDiscovery) copyInstancesLocked(serviceName string) []*Instance {
	instances := d.instances[serviceName]
	cpy := make([]*Instance, len(instances))
	copy(cpy, instances)
	return cpy
}

// Deregister 注销服务实例
func (d *StaticDiscovery) Deregister(ctx context.Context, instanceID string) error {
	d.mu.Lock()
	var instances []*Instance
	var serviceName string
	for name, insts := range d.instances {
		for i, inst := range insts {
			if inst.ID == instanceID {
				d.instances[name] = append(insts[:i], insts[i+1:]...)
				// 如果没有实例了，删除服务
				if len(d.instances[name]) == 0 {
					delete(d.instances, name)
				} else {
					instances = d.copyInstancesLocked(name)
				}
				serviceName = name
				d.mu.Unlock()
				if len(instances) > 0 {
					d.notifyWatchersUnlocked(serviceName, instances)
				}
				return nil
			}
		}
	}
	d.mu.Unlock()

	return nil
}

// Discover 发现服务实例
func (d *StaticDiscovery) Discover(ctx context.Context, serviceName string) ([]*Instance, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	instances, ok := d.instances[serviceName]
	if !ok {
		return nil, errors.New("service not found")
	}

	// 返回副本
	result := make([]*Instance, len(instances))
	copy(result, instances)
	return result, nil
}

// Watch 监听服务变更
func (d *StaticDiscovery) Watch(ctx context.Context, serviceName string) (<-chan []*Instance, error) {
	ch := make(chan []*Instance, 10)

	d.watcherMu.Lock()
	// 检查是否已关闭
	if d.closed {
		d.watcherMu.Unlock()
		close(ch)
		return ch, nil
	}
	d.watchers[serviceName] = append(d.watchers[serviceName], ch)
	d.watcherMu.Unlock()

	// 立即发送当前实例
	go func() {
		defer func() {
			// 忽略向已关闭 channel 发送导致的 panic
			recover()
		}()

		d.mu.RLock()
		instances := d.instances[serviceName]
		cpy := make([]*Instance, len(instances))
		copy(cpy, instances)
		d.mu.RUnlock()

		if len(cpy) > 0 {
			select {
			case ch <- cpy:
			case <-d.closeCh:
			}
		}
	}()

	return ch, nil
}

// notifyWatchersUnlocked 通知观察者 (调用者不持有锁)
func (d *StaticDiscovery) notifyWatchersUnlocked(serviceName string, instances []*Instance) {
	d.watcherMu.Lock()
	defer d.watcherMu.Unlock()

	// 如果已关闭，不发送
	if d.closed {
		return
	}

	for _, ch := range d.watchers[serviceName] {
		select {
		case ch <- instances:
		default:
			// channel full, skip
		}
	}
}

// notifyWatchers 通知观察者
func (d *StaticDiscovery) notifyWatchers(serviceName string) {
	d.mu.RLock()
	instances := d.copyInstancesLocked(serviceName)
	d.mu.RUnlock()

	d.notifyWatchersUnlocked(serviceName, instances)
}

// Close 关闭服务发现
func (d *StaticDiscovery) Close() error {
	d.watcherMu.Lock()
	defer d.watcherMu.Unlock()

	if d.closed {
		return nil
	}

	d.closed = true
	close(d.closeCh)

	for _, watchers := range d.watchers {
		for _, ch := range watchers {
			close(ch)
		}
	}
	d.watchers = make(map[string][]chan []*Instance)

	return nil
}

// AddInstance 添加实例 (便捷方法)
func (d *StaticDiscovery) AddInstance(name, id, host string, port int, metadata map[string]string) {
	instance := &Instance{
		ID:       id,
		Name:     name,
		Host:     host,
		Port:     port,
		Metadata: metadata,
		Healthy:  true,
		Weight:   1,
	}
	d.Register(context.Background(), instance)
}

// RemoveInstance 移除实例 (便捷方法)
func (d *StaticDiscovery) RemoveInstance(instanceID string) {
	d.Deregister(context.Background(), instanceID)
}

// GetInstances 获取所有实例
func (d *StaticDiscovery) GetInstances(serviceName string) []*Instance {
	d.mu.RLock()
	defer d.mu.RUnlock()

	instances := d.instances[serviceName]
	result := make([]*Instance, len(instances))
	copy(result, instances)
	return result
}