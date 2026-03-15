package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"go.etcd.io/etcd/client/v3"
	"go.uber.org/zap"
)

// EtcdDiscovery etcd 服务发现实现
type EtcdDiscovery struct {
	client   *clientv3.Client
	config   EtcdConfig
	logger   *zap.Logger

	// 服务缓存
	services map[string][]*Instance
	cacheMu  sync.RWMutex

	// 观察者
	watchers  map[string][]chan []*Instance
	watcherMu sync.Mutex

	// 前缀
	prefix string

	// 控制
	stopCh   chan struct{}
	running  bool
	runMu    sync.Mutex
}

// EtcdOption etcd 发现选项
type EtcdOption func(*EtcdDiscovery)

// NewEtcdDiscovery 创建 etcd 服务发现
func NewEtcdDiscovery(cfg EtcdConfig, logger *zap.Logger, opts ...EtcdOption) (*EtcdDiscovery, error) {
	// 创建 etcd 客户端配置
	clientConfig := clientv3.Config{
		Endpoints:   cfg.Endpoints,
		DialTimeout: 5 * time.Second,
	}

	// 创建客户端
	client, err := clientv3.New(clientConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create etcd client: %w", err)
	}

	// 设置前缀
	prefix := cfg.Prefix
	if prefix == "" {
		prefix = "/services"
	}

	d := &EtcdDiscovery{
		client:   client,
		config:   cfg,
		logger:   logger,
		services: make(map[string][]*Instance),
		watchers: make(map[string][]chan []*Instance),
		prefix:   prefix,
		stopCh:   make(chan struct{}),
	}

	// 应用选项
	for _, opt := range opts {
		opt(d)
	}

	return d, nil
}

// Register 注册服务实例
func (d *EtcdDiscovery) Register(ctx context.Context, instance *Instance) error {
	if instance.Weight == 0 {
		instance.Weight = 1
	}
	instance.UpdatedAt = time.Now()
	instance.Healthy = true

	// 序列化实例
	data, err := json.Marshal(instance)
	if err != nil {
		return fmt.Errorf("failed to marshal instance: %w", err)
	}

	// 构建键
	key := d.instanceKey(instance.Name, instance.ID)

	// 设置租约 (10秒 TTL)
	lease, err := d.client.Grant(ctx, 10)
	if err != nil {
		return fmt.Errorf("failed to create lease: %w", err)
	}

	// 注册服务
	_, err = d.client.Put(ctx, key, string(data), clientv3.WithLease(lease.ID))
	if err != nil {
		return fmt.Errorf("failed to register service: %w", err)
	}

	// 启动保活
	go d.keepAlive(lease.ID)

	d.logger.Info("service registered",
		zap.String("service", instance.Name),
		zap.String("id", instance.ID),
		zap.String("key", key),
	)

	return nil
}

// keepAlive 保持租约
func (d *EtcdDiscovery) keepAlive(leaseID clientv3.LeaseID) {
	ch, err := d.client.KeepAlive(context.Background(), leaseID)
	if err != nil {
		d.logger.Warn("keep alive failed", zap.Error(err))
		return
	}

	for {
		select {
		case <-d.stopCh:
			return
		case resp, ok := <-ch:
			if !ok {
				return
			}
			if resp == nil {
				return
			}
		}
	}
}

// Deregister 注销服务实例
func (d *EtcdDiscovery) Deregister(ctx context.Context, instanceID string) error {
	// 需要找到对应的服务名
	// 先从缓存中查找
	d.cacheMu.RLock()
	var key string
	for name, instances := range d.services {
		for _, inst := range instances {
			if inst.ID == instanceID {
				key = d.instanceKey(name, instanceID)
				break
			}
		}
		if key != "" {
			break
		}
	}
	d.cacheMu.RUnlock()

	// 如果缓存中没有，尝试从 etcd 中查找
	if key == "" {
		// 获取所有服务
		resp, err := d.client.Get(ctx, d.prefix, clientv3.WithPrefix())
		if err != nil {
			return fmt.Errorf("failed to find instance: %w", err)
		}

		for _, kv := range resp.Kvs {
			var inst Instance
			if err := json.Unmarshal(kv.Value, &inst); err != nil {
				continue
			}
			if inst.ID == instanceID {
				key = string(kv.Key)
				break
			}
		}
	}

	if key == "" {
		return nil // 实例不存在
	}

	// 删除键
	_, err := d.client.Delete(ctx, key)
	if err != nil {
		return fmt.Errorf("failed to deregister service: %w", err)
	}

	d.logger.Info("service deregistered",
		zap.String("id", instanceID),
	)

	return nil
}

// Discover 发现服务实例
func (d *EtcdDiscovery) Discover(ctx context.Context, serviceName string) ([]*Instance, error) {
	// 从缓存获取
	d.cacheMu.RLock()
	if instances, ok := d.services[serviceName]; ok {
		d.cacheMu.RUnlock()
		return instances, nil
	}
	d.cacheMu.RUnlock()

	// 从 etcd 查询
	instances, err := d.fetchInstances(ctx, serviceName)
	if err != nil {
		return nil, err
	}

	// 更新缓存
	d.cacheMu.Lock()
	d.services[serviceName] = instances
	d.cacheMu.Unlock()

	return instances, nil
}

// fetchInstances 从 etcd 获取实例
func (d *EtcdDiscovery) fetchInstances(ctx context.Context, serviceName string) ([]*Instance, error) {
	prefix := d.servicePrefix(serviceName)

	resp, err := d.client.Get(ctx, prefix, clientv3.WithPrefix())
	if err != nil {
		return nil, fmt.Errorf("failed to get instances: %w", err)
	}

	instances := make([]*Instance, 0, len(resp.Kvs))
	for _, kv := range resp.Kvs {
		var inst Instance
		if err := json.Unmarshal(kv.Value, &inst); err != nil {
			d.logger.Warn("failed to unmarshal instance",
				zap.String("key", string(kv.Key)),
				zap.Error(err),
			)
			continue
		}
		inst.Healthy = true // 在 etcd 中的实例认为是健康的
		instances = append(instances, &inst)
	}

	return instances, nil
}

// Watch 监听服务变更
func (d *EtcdDiscovery) Watch(ctx context.Context, serviceName string) (<-chan []*Instance, error) {
	ch := make(chan []*Instance, 10)

	d.watcherMu.Lock()
	d.watchers[serviceName] = append(d.watchers[serviceName], ch)
	d.watcherMu.Unlock()

	// 启动后台监听
	go d.watchService(ctx, serviceName, ch)

	return ch, nil
}

// watchService 监听单个服务
func (d *EtcdDiscovery) watchService(ctx context.Context, serviceName string, ch chan<- []*Instance) {
	prefix := d.servicePrefix(serviceName)

	// 创建监听器
	watchCh := d.client.Watch(ctx, prefix, clientv3.WithPrefix())

	// 先发送当前状态
	instances, err := d.fetchInstances(ctx, serviceName)
	if err == nil && len(instances) > 0 {
		select {
		case ch <- instances:
		default:
		}
	}

	for {
		select {
		case <-d.stopCh:
			return
		case resp, ok := <-watchCh:
			if !ok {
				return
			}

			if err := resp.Err(); err != nil {
				d.logger.Warn("watch error",
					zap.String("service", serviceName),
					zap.Error(err),
				)
				continue
			}

			// 有变更，重新获取实例列表
			instances, err := d.fetchInstances(ctx, serviceName)
			if err != nil {
				d.logger.Warn("failed to fetch instances after change",
					zap.String("service", serviceName),
					zap.Error(err),
				)
				continue
			}

			// 更新缓存
			d.cacheMu.Lock()
			d.services[serviceName] = instances
			d.cacheMu.Unlock()

			// 发送通知
			select {
			case ch <- instances:
			default:
			}
		}
	}
}

// Close 关闭服务发现
func (d *EtcdDiscovery) Close() error {
	d.runMu.Lock()
	defer d.runMu.Unlock()

	if d.running {
		close(d.stopCh)
		d.running = false
	}

	// 关闭所有观察者
	d.watcherMu.Lock()
	for _, watchers := range d.watchers {
		for _, ch := range watchers {
			close(ch)
		}
	}
	d.watchers = make(map[string][]chan []*Instance)
	d.watcherMu.Unlock()

	if d.client != nil {
		return d.client.Close()
	}

	return nil
}

// InvalidateCache 使缓存失效
func (d *EtcdDiscovery) InvalidateCache(serviceName string) {
	d.cacheMu.Lock()
	defer d.cacheMu.Unlock()
	delete(d.services, serviceName)
}

// instanceKey 获取实例键
func (d *EtcdDiscovery) instanceKey(serviceName, instanceID string) string {
	return fmt.Sprintf("%s/%s/%s", d.prefix, serviceName, instanceID)
}

// servicePrefix 获取服务前缀
func (d *EtcdDiscovery) servicePrefix(serviceName string) string {
	return fmt.Sprintf("%s/%s/", d.prefix, serviceName)
}

// GetClient 获取 etcd 客户端 (用于高级操作)
func (d *EtcdDiscovery) GetClient() *clientv3.Client {
	return d.client
}

// RegisterWithLease 使用自定义租约注册
func (d *EtcdDiscovery) RegisterWithLease(ctx context.Context, instance *Instance, ttl int64) (clientv3.LeaseID, error) {
	if instance.Weight == 0 {
		instance.Weight = 1
	}
	instance.UpdatedAt = time.Now()
	instance.Healthy = true

	data, err := json.Marshal(instance)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal instance: %w", err)
	}

	key := d.instanceKey(instance.Name, instance.ID)

	lease, err := d.client.Grant(ctx, ttl)
	if err != nil {
		return 0, fmt.Errorf("failed to create lease: %w", err)
	}

	_, err = d.client.Put(ctx, key, string(data), clientv3.WithLease(lease.ID))
	if err != nil {
		_, _ = d.client.Revoke(ctx, lease.ID)
		return 0, fmt.Errorf("failed to register service: %w", err)
	}

	return lease.ID, nil
}

// RenewLease 续租
func (d *EtcdDiscovery) RenewLease(ctx context.Context, leaseID clientv3.LeaseID) error {
	_, err := d.client.KeepAliveOnce(ctx, leaseID)
	return err
}

// RevokeLease 撤销租约
func (d *EtcdDiscovery) RevokeLease(ctx context.Context, leaseID clientv3.LeaseID) error {
	_, err := d.client.Revoke(ctx, leaseID)
	return err
}

// ListServices 列出所有服务
func (d *EtcdDiscovery) ListServices(ctx context.Context) ([]string, error) {
	resp, err := d.client.Get(ctx, d.prefix, clientv3.WithPrefix(), clientv3.WithKeysOnly())
	if err != nil {
		return nil, err
	}

	serviceSet := make(map[string]struct{})
	for _, kv := range resp.Kvs {
		// 解析服务名: /prefix/serviceName/instanceID
		key := string(kv.Key)
		relPath := key[len(d.prefix)+1:] // 移除前缀
		for i, c := range relPath {
			if c == '/' {
				serviceName := relPath[:i]
				serviceSet[serviceName] = struct{}{}
				break
			}
		}
	}

	services := make([]string, 0, len(serviceSet))
	for name := range serviceSet {
		services = append(services, name)
	}

	return services, nil
}

// notifyWatchers 通知观察者
func (d *EtcdDiscovery) notifyWatchers(serviceName string, instances []*Instance) {
	d.watcherMu.Lock()
	defer d.watcherMu.Unlock()

	for _, ch := range d.watchers[serviceName] {
		select {
		case ch <- instances:
		default:
		}
	}
}