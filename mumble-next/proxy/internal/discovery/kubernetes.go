package discovery

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// KubernetesDiscovery Kubernetes 服务发现实现
type KubernetesDiscovery struct {
	client    *kubernetes.Clientset
	config    KubernetesConfig
	logger    *zap.Logger

	// 服务缓存
	services map[string][]*Instance
	cacheMu  sync.RWMutex

	// 观察者
	watchers  map[string][]chan []*Instance
	watcherMu sync.Mutex

	// 控制
	stopCh   chan struct{}
	running  bool
	runMu    sync.Mutex
}

// KubernetesOption Kubernetes 发现选项
type KubernetesOption func(*KubernetesDiscovery)

// NewKubernetesDiscovery 创建 Kubernetes 服务发现
func NewKubernetesDiscovery(cfg KubernetesConfig, logger *zap.Logger, opts ...KubernetesOption) (*KubernetesDiscovery, error) {
	// 创建 Kubernetes 配置
	var restConfig *rest.Config
	var err error

	if cfg.Kubeconfig != "" {
		// 使用指定的 kubeconfig 文件
		restConfig, err = clientcmd.BuildConfigFromFlags("", cfg.Kubeconfig)
	} else {
		// 使用集群内配置
		restConfig, err = rest.InClusterConfig()
	}

	if err != nil {
		return nil, fmt.Errorf("failed to get kubernetes config: %w", err)
	}

	// 创建客户端
	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	// 设置默认命名空间
	namespace := cfg.Namespace
	if namespace == "" {
		namespace = "default"
	}

	d := &KubernetesDiscovery{
		client:   client,
		config:   cfg,
		logger:   logger,
		services: make(map[string][]*Instance),
		watchers: make(map[string][]chan []*Instance),
		stopCh:   make(chan struct{}),
	}
	d.config.Namespace = namespace

	// 应用选项
	for _, opt := range opts {
		opt(d)
	}

	return d, nil
}

// Register 注册服务实例 (Kubernetes 中通常由 Service 自动管理)
func (d *KubernetesDiscovery) Register(ctx context.Context, instance *Instance) error {
	// Kubernetes 中服务注册通常通过 Deployment/Service 完成
	// 这里提供一个简单的实现，创建一个 Endpoints
	return fmt.Errorf("kubernetes discovery does not support manual registration, use Service instead")
}

// Deregister 注销服务实例
func (d *KubernetesDiscovery) Deregister(ctx context.Context, instanceID string) error {
	return fmt.Errorf("kubernetes discovery does not support manual deregistration")
}

// Discover 发现服务实例
func (d *KubernetesDiscovery) Discover(ctx context.Context, serviceName string) ([]*Instance, error) {
	// 从缓存获取
	d.cacheMu.RLock()
	if instances, ok := d.services[serviceName]; ok {
		d.cacheMu.RUnlock()
		return instances, nil
	}
	d.cacheMu.RUnlock()

	// 从 Kubernetes 查询
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

// fetchInstances 从 Kubernetes 获取实例
func (d *KubernetesDiscovery) fetchInstances(ctx context.Context, serviceName string) ([]*Instance, error) {
	// 尝试使用 EndpointSlice (新 API)
	instances, err := d.fetchFromEndpointSlice(ctx, serviceName)
	if err == nil && len(instances) > 0 {
		return instances, nil
	}

	// 回退到 Endpoints (旧 API)
	return d.fetchFromEndpoints(ctx, serviceName)
}

// fetchFromEndpointSlice 从 EndpointSlice 获取实例
func (d *KubernetesDiscovery) fetchFromEndpointSlice(ctx context.Context, serviceName string) ([]*Instance, error) {
	slices, err := d.client.DiscoveryV1().EndpointSlices(d.config.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labels.Set{"kubernetes.io/service-name": serviceName}.String(),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list endpoint slices: %w", err)
	}

	instances := make([]*Instance, 0)
	for _, slice := range slices.Items {
		for _, endpoint := range slice.Endpoints {
			if endpoint.Conditions.Ready == nil || !*endpoint.Conditions.Ready {
				continue
			}

			for _, addr := range endpoint.Addresses {
				// 获取端口
				port := d.getServicePort(ctx, serviceName)
				if port == 0 {
					port = 64738 // 默认端口
				}

				instance := &Instance{
					ID:       fmt.Sprintf("%s-%s", slice.Name, addr),
					Name:     serviceName,
					Host:     addr,
					Port:     port,
					Healthy:  true,
					Weight:   1,
					Metadata: make(map[string]string),
				}

				// 添加元数据
				if endpoint.Zone != nil {
					instance.Metadata["zone"] = *endpoint.Zone
				}
				if endpoint.NodeName != nil {
					instance.Metadata["node"] = *endpoint.NodeName
				}
				instance.Metadata["endpointSlice"] = slice.Name

				instances = append(instances, instance)
			}
		}
	}

	return instances, nil
}

// fetchFromEndpoints 从 Endpoints 获取实例
func (d *KubernetesDiscovery) fetchFromEndpoints(ctx context.Context, serviceName string) ([]*Instance, error) {
	endpoints, err := d.client.CoreV1().Endpoints(d.config.Namespace).Get(ctx, serviceName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get endpoints: %w", err)
	}

	instances := make([]*Instance, 0)
	for _, subset := range endpoints.Subsets {
		// 获取端口
		port := 64738 // 默认端口
		if len(subset.Ports) > 0 {
			port = int(subset.Ports[0].Port)
		}

		// 处理就绪地址
		for _, addr := range subset.Addresses {
			instance := &Instance{
				ID:       fmt.Sprintf("%s-%s", serviceName, addr.IP),
				Name:     serviceName,
				Host:     addr.IP,
				Port:     port,
				Healthy:  true,
				Weight:   1,
				Metadata: make(map[string]string),
			}

			if addr.NodeName != nil {
				instance.Metadata["node"] = *addr.NodeName
			}
			if addr.TargetRef != nil {
				instance.Metadata["pod"] = addr.TargetRef.Name
			}

			instances = append(instances, instance)
		}

		// 处理未就绪地址 (可选)
		for _, addr := range subset.NotReadyAddresses {
			instance := &Instance{
				ID:       fmt.Sprintf("%s-%s", serviceName, addr.IP),
				Name:     serviceName,
				Host:     addr.IP,
				Port:     port,
				Healthy:  false,
				Weight:   1,
				Metadata: make(map[string]string),
			}

			if addr.NodeName != nil {
				instance.Metadata["node"] = *addr.NodeName
			}

			instances = append(instances, instance)
		}
	}

	return instances, nil
}

// getServicePort 获取服务端口
func (d *KubernetesDiscovery) getServicePort(ctx context.Context, serviceName string) int {
	svc, err := d.client.CoreV1().Services(d.config.Namespace).Get(ctx, serviceName, metav1.GetOptions{})
	if err != nil {
		return 0
	}

	if len(svc.Spec.Ports) > 0 {
		return int(svc.Spec.Ports[0].Port)
	}

	return 0
}

// Watch 监听服务变更
func (d *KubernetesDiscovery) Watch(ctx context.Context, serviceName string) (<-chan []*Instance, error) {
	ch := make(chan []*Instance, 10)

	d.watcherMu.Lock()
	d.watchers[serviceName] = append(d.watchers[serviceName], ch)
	d.watcherMu.Unlock()

	// 启动后台监听
	go d.watchService(ctx, serviceName, ch)

	return ch, nil
}

// watchService 监听单个服务
func (d *KubernetesDiscovery) watchService(ctx context.Context, serviceName string, ch chan<- []*Instance) {
	// 先发送当前状态
	instances, err := d.fetchInstances(ctx, serviceName)
	if err == nil {
		select {
		case ch <- instances:
		default:
		}
	}

	// 尝试监听 EndpointSlice
	watcher, err := d.client.DiscoveryV1().EndpointSlices(d.config.Namespace).Watch(ctx, metav1.ListOptions{
		LabelSelector: labels.Set{"kubernetes.io/service-name": serviceName}.String(),
	})
	if err != nil {
		// 回退到监听 Endpoints
		watcher, err = d.client.CoreV1().Endpoints(d.config.Namespace).Watch(ctx, metav1.ListOptions{
			FieldSelector: fmt.Sprintf("metadata.name=%s", serviceName),
		})
		if err != nil {
			d.logger.Warn("failed to watch service",
				zap.String("service", serviceName),
				zap.Error(err),
			)
			return
		}
	}
	defer watcher.Stop()

	for {
		select {
		case <-d.stopCh:
			return
		case event, ok := <-watcher.ResultChan():
			if !ok {
				// 重新建立监听
				time.Sleep(time.Second)
				go d.watchService(ctx, serviceName, ch)
				return
			}

			switch event.Type {
			case watch.Added, watch.Modified, watch.Deleted:
				// 重新获取实例列表
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
}

// Close 关闭服务发现
func (d *KubernetesDiscovery) Close() error {
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

	return nil
}

// InvalidateCache 使缓存失效
func (d *KubernetesDiscovery) InvalidateCache(serviceName string) {
	d.cacheMu.Lock()
	defer d.cacheMu.Unlock()
	delete(d.services, serviceName)
}

// GetClient 获取 Kubernetes 客户端 (用于高级操作)
func (d *KubernetesDiscovery) GetClient() *kubernetes.Clientset {
	return d.client
}

// ListServices 列出所有服务
func (d *KubernetesDiscovery) ListServices(ctx context.Context) ([]string, error) {
	services, err := d.client.CoreV1().Services(d.config.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list services: %w", err)
	}

	names := make([]string, 0, len(services.Items))
	for _, svc := range services.Items {
		names = append(names, svc.Name)
	}

	return names, nil
}

// ListServicesBySelector 按标签选择器列出服务
func (d *KubernetesDiscovery) ListServicesBySelector(ctx context.Context, selector labels.Selector) ([]string, error) {
	services, err := d.client.CoreV1().Services(d.config.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector.String(),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list services by selector: %w", err)
	}

	names := make([]string, 0, len(services.Items))
	for _, svc := range services.Items {
		names = append(names, svc.Name)
	}

	return names, nil
}

// GetService 获取服务详情
func (d *KubernetesDiscovery) GetService(ctx context.Context, serviceName string) (*corev1.Service, error) {
	return d.client.CoreV1().Services(d.config.Namespace).Get(ctx, serviceName, metav1.GetOptions{})
}

// notifyWatchers 通知观察者
func (d *KubernetesDiscovery) notifyWatchers(serviceName string, instances []*Instance) {
	d.watcherMu.Lock()
	defer d.watcherMu.Unlock()

	for _, ch := range d.watchers[serviceName] {
		select {
		case ch <- instances:
		default:
		}
	}
}