// Package registry 整合服务发现和负载均衡
package registry

import (
	"context"
	"errors"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/mumble/mumble-next/proxy/internal/balancer"
	"github.com/mumble/mumble-next/proxy/internal/discovery"
)

// ErrNoHealthyInstance 没有健康的实例
var ErrNoHealthyInstance = errors.New("no healthy instance available")

// Registry 服务注册中心
type Registry struct {
	discovery discovery.Discovery
	balancer  balancer.Balancer
	logger    *zap.Logger

	// 缓存
	cache     map[string][]*discovery.Instance
	cacheMu   sync.RWMutex

	// 健康检查
	healthChecker *HealthChecker
}

// Config 注册中心配置
type Config struct {
	Discovery discovery.Config
	Balancer  balancer.Config
}

// NewRegistry 创建服务注册中心
func NewRegistry(d discovery.Discovery, b balancer.Balancer, logger *zap.Logger) *Registry {
	r := &Registry{
		discovery: d,
		balancer:  b,
		logger:    logger,
		cache:     make(map[string][]*discovery.Instance),
	}

	r.healthChecker = NewHealthChecker(d, logger)
	return r
}

// Register 注册服务实例
func (r *Registry) Register(ctx context.Context, instance *discovery.Instance) error {
	return r.discovery.Register(ctx, instance)
}

// Deregister 注销服务实例
func (r *Registry) Deregister(ctx context.Context, instanceID string) error {
	return r.discovery.Deregister(ctx, instanceID)
}

// Select 选择一个服务实例
func (r *Registry) Select(ctx context.Context, serviceName string) (*discovery.Instance, error) {
	// 尝试从缓存获取
	r.cacheMu.RLock()
	cached, ok := r.cache[serviceName]
	r.cacheMu.RUnlock()

	var instances []*discovery.Instance
	if ok && len(cached) > 0 {
		instances = cached
	} else {
		// 从服务发现获取
		var err error
		instances, err = r.discovery.Discover(ctx, serviceName)
		if err != nil {
			return nil, err
		}

		// 更新缓存
		r.cacheMu.Lock()
		r.cache[serviceName] = instances
		r.cacheMu.Unlock()
	}

	// 过滤健康实例
	healthy := r.filterHealthy(instances)
	if len(healthy) == 0 {
		return nil, ErrNoHealthyInstance
	}

	// 使用负载均衡器选择
	return r.balancer.Select(healthy)
}

// SelectWithKey 使用键选择服务实例 (用于一致性哈希)
func (r *Registry) SelectWithKey(ctx context.Context, serviceName, key string) (*discovery.Instance, error) {
	instances, err := r.discovery.Discover(ctx, serviceName)
	if err != nil {
		return nil, err
	}

	healthy := r.filterHealthy(instances)
	if len(healthy) == 0 {
		return nil, ErrNoHealthyInstance
	}

	// 如果负载均衡器支持一致性哈希
	if ch, ok := r.balancer.(interface {
		SelectWithKey([]*discovery.Instance, string) (*discovery.Instance, error)
	}); ok {
		return ch.SelectWithKey(healthy, key)
	}

	return r.balancer.Select(healthy)
}

// Discover 发现服务实例
func (r *Registry) Discover(ctx context.Context, serviceName string) ([]*discovery.Instance, error) {
	return r.discovery.Discover(ctx, serviceName)
}

// Watch 监听服务变更
func (r *Registry) Watch(ctx context.Context, serviceName string) (<-chan []*discovery.Instance, error) {
	return r.discovery.Watch(ctx, serviceName)
}

// StartHealthCheck 启动健康检查
func (r *Registry) StartHealthCheck(interval time.Duration) {
	r.healthChecker.Start(interval)
}

// StopHealthCheck 停止健康检查
func (r *Registry) StopHealthCheck() {
	r.healthChecker.Stop()
}

// InvalidateCache 使缓存失效
func (r *Registry) InvalidateCache(serviceName string) {
	r.cacheMu.Lock()
	defer r.cacheMu.Unlock()
	delete(r.cache, serviceName)
}

// Close 关闭注册中心
func (r *Registry) Close() error {
	r.healthChecker.Stop()
	return r.discovery.Close()
}

// filterHealthy 过滤健康实例
func (r *Registry) filterHealthy(instances []*discovery.Instance) []*discovery.Instance {
	healthy := make([]*discovery.Instance, 0, len(instances))
	for _, inst := range instances {
		if inst.Healthy {
			healthy = append(healthy, inst)
		}
	}
	return healthy
}

// GetDiscovery 获取服务发现实例
func (r *Registry) GetDiscovery() discovery.Discovery {
	return r.discovery
}

// GetBalancer 获取负载均衡器
func (r *Registry) GetBalancer() balancer.Balancer {
	return r.balancer
}