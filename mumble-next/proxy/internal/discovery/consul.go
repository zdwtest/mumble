package discovery

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/hashicorp/consul/api"
	"go.uber.org/zap"
)

// ConsulDiscovery Consul 服务发现实现
type ConsulDiscovery struct {
	client    *api.Client
	config    ConsulConfig
	logger    *zap.Logger

	// 服务缓存
	services  map[string][]*Instance
	cacheMu   sync.RWMutex

	// 观察者
	watchers  map[string][]chan []*Instance
	watcherMu sync.Mutex

	// 健康检查通道
	healthCh  chan string

	// 控制通道
	stopCh    chan struct{}
	running   bool
	runningMu sync.Mutex
}

// ConsulOption Consul 发现选项
type ConsulOption func(*ConsulDiscovery)

// NewConsulDiscovery 创建 Consul 服务发现
func NewConsulDiscovery(cfg ConsulConfig, logger *zap.Logger, opts ...ConsulOption) (*ConsulDiscovery, error) {
	// 创建 Consul 客户端配置
	consulConfig := api.DefaultConfig()
	if cfg.Address != "" {
		consulConfig.Address = cfg.Address
	}
	if cfg.Token != "" {
		consulConfig.Token = cfg.Token
	}

	// 创建客户端
	client, err := api.NewClient(consulConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create consul client: %w", err)
	}

	d := &ConsulDiscovery{
		client:   client,
		config:   cfg,
		logger:   logger,
		services: make(map[string][]*Instance),
		watchers: make(map[string][]chan []*Instance),
		healthCh: make(chan string, 100),
		stopCh:   make(chan struct{}),
	}

	// 应用选项
	for _, opt := range opts {
		opt(d)
	}

	return d, nil
}

// Register 注册服务实例
func (d *ConsulDiscovery) Register(ctx context.Context, instance *Instance) error {
	registration := &api.AgentServiceRegistration{
		ID:      instance.ID,
		Name:    instance.Name,
		Address: instance.Host,
		Port:    instance.Port,
		Meta:    instance.Metadata,
	}

	// 设置健康检查
	if instance.Healthy {
		registration.Check = &api.AgentServiceCheck{
			HTTP:     fmt.Sprintf("http://%s:%d/health", instance.Host, instance.Port),
			Interval: "10s",
			Timeout:  "5s",
		}
	}

	// 设置权重 (通过 Meta 传递)
	if registration.Meta == nil {
		registration.Meta = make(map[string]string)
	}
	registration.Meta["weight"] = fmt.Sprintf("%d", instance.Weight)

	err := d.client.Agent().ServiceRegister(registration)
	if err != nil {
		return fmt.Errorf("failed to register service: %w", err)
	}

	d.logger.Info("service registered",
		zap.String("service", instance.Name),
		zap.String("id", instance.ID),
	)

	return nil
}

// Deregister 注销服务实例
func (d *ConsulDiscovery) Deregister(ctx context.Context, instanceID string) error {
	err := d.client.Agent().ServiceDeregister(instanceID)
	if err != nil {
		return fmt.Errorf("failed to deregister service: %w", err)
	}

	d.logger.Info("service deregistered",
		zap.String("id", instanceID),
	)

	return nil
}

// Discover 发现服务实例
func (d *ConsulDiscovery) Discover(ctx context.Context, serviceName string) ([]*Instance, error) {
	// 从缓存获取
	d.cacheMu.RLock()
	if instances, ok := d.services[serviceName]; ok {
		d.cacheMu.RUnlock()
		return instances, nil
	}
	d.cacheMu.RUnlock()

	// 从 Consul 查询
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

// fetchInstances 从 Consul 获取实例
func (d *ConsulDiscovery) fetchInstances(ctx context.Context, serviceName string) ([]*Instance, error) {
	// 构建查询选项
	opts := &api.QueryOptions{}
	if d.config.Namespace != "" {
		opts.Namespace = d.config.Namespace
	}
	opts = opts.WithContext(ctx)

	// 查询健康的服务实例
	services, _, err := d.client.Health().Service(serviceName, "", true, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to query service: %w", err)
	}

	instances := make([]*Instance, 0, len(services))
	for _, service := range services {
		instance := &Instance{
			ID:       service.Service.ID,
			Name:     service.Service.Service,
			Host:     service.Service.Address,
			Port:     service.Service.Port,
			Metadata: service.Service.Meta,
			Healthy:  true, // 已经是健康检查通过的
			Weight:   1,
		}

		// 解析权重
		if weight, ok := service.Service.Meta["weight"]; ok {
			var w int
			_, _ = fmt.Sscanf(weight, "%d", &w)
			if w > 0 {
				instance.Weight = w
			}
		}

		instances = append(instances, instance)
	}

	return instances, nil
}

// Watch 监听服务变更
func (d *ConsulDiscovery) Watch(ctx context.Context, serviceName string) (<-chan []*Instance, error) {
	ch := make(chan []*Instance, 10)

	d.watcherMu.Lock()
	d.watchers[serviceName] = append(d.watchers[serviceName], ch)
	d.watcherMu.Unlock()

	// 启动后台监听
	go d.watchService(ctx, serviceName, ch)

	return ch, nil
}

// watchService 监听单个服务
func (d *ConsulDiscovery) watchService(ctx context.Context, serviceName string, ch chan<- []*Instance) {
	var lastIndex uint64

	for {
		select {
		case <-ctx.Done():
			return
		case <-d.stopCh:
			return
		default:
		}

		// 构建查询选项 (阻塞查询)
		opts := &api.QueryOptions{
			WaitIndex: lastIndex,
			WaitTime:  30 * time.Second,
		}
		if d.config.Namespace != "" {
			opts.Namespace = d.config.Namespace
		}
		opts = opts.WithContext(ctx)

		// 查询服务
		services, meta, err := d.client.Health().Service(serviceName, "", true, opts)
		if err != nil {
			d.logger.Warn("watch query failed",
				zap.String("service", serviceName),
				zap.Error(err),
			)
			time.Sleep(time.Second)
			continue
		}

		// 检查是否有变更
		if meta.LastIndex == lastIndex {
			continue
		}
		lastIndex = meta.LastIndex

		// 转换实例
		instances := make([]*Instance, 0, len(services))
		for _, service := range services {
			instance := &Instance{
				ID:       service.Service.ID,
				Name:     service.Service.Service,
				Host:     service.Service.Address,
				Port:     service.Service.Port,
				Metadata: service.Service.Meta,
				Healthy:  true,
				Weight:   1,
			}

			if weight, ok := service.Service.Meta["weight"]; ok {
				var w int
				_, _ = fmt.Sscanf(weight, "%d", &w)
				if w > 0 {
					instance.Weight = w
				}
			}

			instances = append(instances, instance)
		}

		// 更新缓存
		d.cacheMu.Lock()
		d.services[serviceName] = instances
		d.cacheMu.Unlock()

		// 发送通知
		select {
		case ch <- instances:
		default:
			// channel full, skip
		}
	}
}

// Close 关闭服务发现
func (d *ConsulDiscovery) Close() error {
	d.runningMu.Lock()
	defer d.runningMu.Unlock()

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

	return nil
}

// InvalidateCache 使缓存失效
func (d *ConsulDiscovery) InvalidateCache(serviceName string) {
	d.cacheMu.Lock()
	defer d.cacheMu.Unlock()
	delete(d.services, serviceName)
}

// StartHealthWatcher 启动健康状态监听
func (d *ConsulDiscovery) StartHealthWatcher(ctx context.Context) {
	d.runningMu.Lock()
	if d.running {
		d.runningMu.Unlock()
		return
	}
	d.running = true
	d.stopCh = make(chan struct{})
	d.runningMu.Unlock()

	go d.healthWatchLoop(ctx)
}

// healthWatchLoop 健康状态监听循环
func (d *ConsulDiscovery) healthWatchLoop(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-d.stopCh:
			return
		case <-ticker.C:
			d.checkHealth(ctx)
		}
	}
}

// checkHealth 检查健康状态
func (d *ConsulDiscovery) checkHealth(ctx context.Context) {
	d.cacheMu.RLock()
	serviceNames := make([]string, 0, len(d.services))
	for name := range d.services {
		serviceNames = append(serviceNames, name)
	}
	d.cacheMu.RUnlock()

	for _, name := range serviceNames {
		instances, err := d.fetchInstances(ctx, name)
		if err != nil {
			d.logger.Warn("health check failed",
				zap.String("service", name),
				zap.Error(err),
			)
			continue
		}

		d.cacheMu.Lock()
		d.services[name] = instances
		d.cacheMu.Unlock()

		// 通知观察者
		d.notifyWatchers(name, instances)
	}
}

// notifyWatchers 通知观察者
func (d *ConsulDiscovery) notifyWatchers(serviceName string, instances []*Instance) {
	d.watcherMu.Lock()
	defer d.watcherMu.Unlock()

	for _, ch := range d.watchers[serviceName] {
		select {
		case ch <- instances:
		default:
		}
	}
}

// GetClient 获取 Consul 客户端 (用于高级操作)
func (d *ConsulDiscovery) GetClient() *api.Client {
	return d.client
}