// Package discovery 提供服务发现抽象接口和实现
package discovery

import (
	"context"
	"time"
)

// Instance 表示一个服务实例
type Instance struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Host      string            `json:"host"`
	Port      int               `json:"port"`
	Metadata  map[string]string `json:"metadata"`
	Healthy   bool              `json:"healthy"`
	Weight    int               `json:"weight"`
	UpdatedAt time.Time         `json:"updated_at"`
}

// Discovery 服务发现接口
type Discovery interface {
	// Register 注册服务实例
	Register(ctx context.Context, instance *Instance) error

	// Deregister 注销服务实例
	Deregister(ctx context.Context, instanceID string) error

	// Discover 发现服务实例
	Discover(ctx context.Context, serviceName string) ([]*Instance, error)

	// Watch 监听服务变更
	Watch(ctx context.Context, serviceName string) (<-chan []*Instance, error)

	// Close 关闭服务发现
	Close() error
}

// HealthChecker 健康检查接口
type HealthChecker interface {
	// Check 检查实例健康状态
	Check(ctx context.Context, instance *Instance) error
}

// Config 服务发现配置
type Config struct {
	// Type 服务发现类型: static, consul, etcd, k8s
	Type string `mapstructure:"type"`

	// RefreshInterval 刷新间隔
	RefreshInterval time.Duration `mapstructure:"refresh_interval"`

	// HealthCheckInterval 健康检查间隔
	HealthCheckInterval time.Duration `mapstructure:"health_check_interval"`

	// Consul Consul 配置
	Consul ConsulConfig `mapstructure:"consul"`

	// Etcd Etcd 配置
	Etcd EtcdConfig `mapstructure:"etcd"`

	// Kubernetes Kubernetes 配置
	Kubernetes KubernetesConfig `mapstructure:"kubernetes"`

	// Static 静态配置
	Static StaticConfig `mapstructure:"static"`
}

// ConsulConfig Consul 配置
type ConsulConfig struct {
	Enabled   bool   `mapstructure:"enabled"`
	Address   string `mapstructure:"address"`
	Token     string `mapstructure:"token"`
	Namespace string `mapstructure:"namespace"`
}

// EtcdConfig Etcd 配置
type EtcdConfig struct {
	Enabled   bool     `mapstructure:"enabled"`
	Endpoints []string `mapstructure:"endpoints"`
	Prefix    string   `mapstructure:"prefix"`
}

// KubernetesConfig Kubernetes 配置
type KubernetesConfig struct {
	Enabled    bool   `mapstructure:"enabled"`
	Namespace  string `mapstructure:"namespace"`
	ServiceName string `mapstructure:"service_name"`
}

// StaticConfig 静态配置
type StaticConfig struct {
	Instances []*Instance `mapstructure:"instances"`
}

// DefaultConfig 返回默认配置
func DefaultConfig() Config {
	return Config{
		Type:                "static",
		RefreshInterval:     30 * time.Second,
		HealthCheckInterval: 10 * time.Second,
	}
}