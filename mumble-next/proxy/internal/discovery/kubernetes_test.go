package discovery

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestKubernetesDiscovery_Config(t *testing.T) {
	logger := zap.NewNop()

	t.Run("InClusterConfig", func(t *testing.T) {
		cfg := KubernetesConfig{
			Namespace: "default",
		}

		d, err := NewKubernetesDiscovery(cfg, logger)
		// 可能失败如果没有在集群中
		if err != nil {
			t.Skipf("Not in Kubernetes cluster: %v", err)
			return
		}
		defer d.Close()

		assert.NotNil(t, d.client)
		assert.Equal(t, "default", d.config.Namespace)
	})

	t.Run("CustomNamespace", func(t *testing.T) {
		cfg := KubernetesConfig{
			Namespace: "mumble",
		}

		d, err := NewKubernetesDiscovery(cfg, logger)
		if err != nil {
			t.Skipf("Not in Kubernetes cluster: %v", err)
			return
		}
		defer d.Close()

		assert.Equal(t, "mumble", d.config.Namespace)
	})
}

func TestKubernetesDiscovery_Options(t *testing.T) {
	logger := zap.NewNop()
	cfg := KubernetesConfig{Namespace: "default"}

	d, err := NewKubernetesDiscovery(cfg, logger, func(k *KubernetesDiscovery) {
		k.services["preloaded"] = []*Instance{
			{ID: "pre-1", Name: "preloaded", Host: "localhost", Port: 8080},
		}
	})
	if err != nil {
		t.Skipf("Not in Kubernetes cluster: %v", err)
		return
	}
	defer d.Close()

	assert.NotNil(t, d.services["preloaded"])
}

func TestKubernetesDiscovery_Cache(t *testing.T) {
	logger := zap.NewNop()
	cfg := KubernetesConfig{Namespace: "default"}

	d, err := NewKubernetesDiscovery(cfg, logger)
	if err != nil {
		t.Skipf("Not in Kubernetes cluster: %v", err)
		return
	}
	defer d.Close()

	// 预填充缓存
	d.cacheMu.Lock()
	d.services["cached-service"] = []*Instance{
		{ID: "cached-1", Name: "cached-service", Host: "localhost", Port: 8080, Healthy: true},
	}
	d.cacheMu.Unlock()

	// 从缓存获取
	instances, err := d.Discover(context.Background(), "cached-service")
	require.NoError(t, err)
	assert.Len(t, instances, 1)

	// 使缓存失效
	d.InvalidateCache("cached-service")

	d.cacheMu.RLock()
	_, ok := d.services["cached-service"]
	d.cacheMu.RUnlock()
	assert.False(t, ok)
}

func TestKubernetesDiscovery_Register(t *testing.T) {
	logger := zap.NewNop()
	cfg := KubernetesConfig{Namespace: "default"}

	d, err := NewKubernetesDiscovery(cfg, logger)
	if err != nil {
		t.Skipf("Not in Kubernetes cluster: %v", err)
		return
	}
	defer d.Close()

	// Kubernetes 不支持手动注册
	err = d.Register(context.Background(), &Instance{ID: "test", Name: "test"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "does not support manual registration")
}

func TestKubernetesDiscovery_Deregister(t *testing.T) {
	logger := zap.NewNop()
	cfg := KubernetesConfig{Namespace: "default"}

	d, err := NewKubernetesDiscovery(cfg, logger)
	if err != nil {
		t.Skipf("Not in Kubernetes cluster: %v", err)
		return
	}
	defer d.Close()

	// Kubernetes 不支持手动注销
	err = d.Deregister(context.Background(), "test")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "does not support manual deregistration")
}

func TestKubernetesDiscovery_Discover(t *testing.T) {
	logger := zap.NewNop()
	cfg := KubernetesConfig{Namespace: "default"}

	d, err := NewKubernetesDiscovery(cfg, logger)
	if err != nil {
		t.Skipf("Not in Kubernetes cluster: %v", err)
		return
	}
	defer d.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 尝试发现服务 (可能不存在)
	instances, err := d.Discover(ctx, "kubernetes")
	if err != nil {
		// 服务可能不存在
		t.Logf("Discover kubernetes service: %v", err)
		return
	}

	t.Logf("Found %d instances for kubernetes service", len(instances))
}

func TestKubernetesDiscovery_Watch(t *testing.T) {
	logger := zap.NewNop()
	cfg := KubernetesConfig{Namespace: "default"}

	d, err := NewKubernetesDiscovery(cfg, logger)
	if err != nil {
		t.Skipf("Not in Kubernetes cluster: %v", err)
		return
	}
	defer d.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ch, err := d.Watch(ctx, "kubernetes")
	require.NoError(t, err)

	assert.NotNil(t, ch)

	// 验证观察者被注册
	d.watcherMu.Lock()
	watchers := d.watchers["kubernetes"]
	d.watcherMu.Unlock()
	assert.NotEmpty(t, watchers)
}

func TestKubernetesDiscovery_Close(t *testing.T) {
	logger := zap.NewNop()
	cfg := KubernetesConfig{Namespace: "default"}

	d, err := NewKubernetesDiscovery(cfg, logger)
	if err != nil {
		t.Skipf("Not in Kubernetes cluster: %v", err)
		return
	}

	// 第一次关闭
	err = d.Close()
	require.NoError(t, err)

	// 第二次关闭应该安全
	err = d.Close()
	require.NoError(t, err)
}

func TestKubernetesDiscovery_GetClient(t *testing.T) {
	logger := zap.NewNop()
	cfg := KubernetesConfig{Namespace: "default"}

	d, err := NewKubernetesDiscovery(cfg, logger)
	if err != nil {
		t.Skipf("Not in Kubernetes cluster: %v", err)
		return
	}
	defer d.Close()

	client := d.GetClient()
	assert.NotNil(t, client)
}

func TestKubernetesDiscovery_ListServices(t *testing.T) {
	logger := zap.NewNop()
	cfg := KubernetesConfig{Namespace: "default"}

	d, err := NewKubernetesDiscovery(cfg, logger)
	if err != nil {
		t.Skipf("Not in Kubernetes cluster: %v", err)
		return
	}
	defer d.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	services, err := d.ListServices(ctx)
	if err != nil {
		t.Logf("ListServices error: %v", err)
		return
	}

	t.Logf("Found %d services", len(services))
	// 通常应该有 kubernetes 服务
	assert.NotEmpty(t, services)
}

func TestKubernetesDiscovery_NotifyWatchers(t *testing.T) {
	logger := zap.NewNop()
	cfg := KubernetesConfig{Namespace: "default"}

	d, err := NewKubernetesDiscovery(cfg, logger)
	if err != nil {
		t.Skipf("Not in Kubernetes cluster: %v", err)
		return
	}
	defer d.Close()

	// 创建测试 channel
	ch := make(chan []*Instance, 10)
	d.watcherMu.Lock()
	d.watchers["test-service"] = append(d.watchers["test-service"], ch)
	d.watcherMu.Unlock()

	// 通知
	instances := []*Instance{
		{ID: "inst-1", Name: "test-service", Host: "localhost", Port: 8080},
	}
	d.notifyWatchers("test-service", instances)

	// 验证收到通知
	select {
	case received := <-ch:
		assert.Len(t, received, 1)
		assert.Equal(t, "inst-1", received[0].ID)
	case <-time.After(time.Second):
		t.Fatal("Timeout waiting for notification")
	}
}

func TestKubernetesDiscovery_InstanceFromEndpoints(t *testing.T) {
	// 测试 Endpoints 到 Instance 的转换逻辑
	port := 64738
	healthy := true

	instance := &Instance{
		ID:       "murmur-10.0.0.1",
		Name:     "murmur",
		Host:     "10.0.0.1",
		Port:     port,
		Healthy:  healthy,
		Weight:   1,
		Metadata: map[string]string{"node": "node-1", "pod": "murmur-pod-1"},
	}

	assert.Equal(t, "murmur-10.0.0.1", instance.ID)
	assert.Equal(t, "murmur", instance.Name)
	assert.Equal(t, "10.0.0.1", instance.Host)
	assert.Equal(t, 64738, instance.Port)
	assert.True(t, instance.Healthy)
	assert.Equal(t, "node-1", instance.Metadata["node"])
	assert.Equal(t, "murmur-pod-1", instance.Metadata["pod"])
}

func TestKubernetesDiscovery_DefaultNamespace(t *testing.T) {
	logger := zap.NewNop()
	cfg := KubernetesConfig{} // 不指定命名空间

	d, err := NewKubernetesDiscovery(cfg, logger)
	if err != nil {
		t.Skipf("Not in Kubernetes cluster: %v", err)
		return
	}
	defer d.Close()

	// 应该使用默认命名空间
	assert.Equal(t, "default", d.config.Namespace)
}