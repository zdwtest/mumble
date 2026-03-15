package config

import (
	"fmt"
	"time"

	"github.com/spf13/viper"
)

// Config 主配置结构
type Config struct {
	Server   ServerConfig   `mapstructure:"server"`
	Murmur   MurmurConfig   `mapstructure:"murmur"`
	Backends []BackendConfig `mapstructure:"backends"`
	Auth     AuthConfig     `mapstructure:"auth"`
	Metrics  MetricsConfig  `mapstructure:"metrics"`
	Logging  LoggingConfig  `mapstructure:"logging"`
	Limits   LimitsConfig   `mapstructure:"limits"`
}

// ServerConfig 服务器配置
type ServerConfig struct {
	Host         string        `mapstructure:"host"`
	Port         int           `mapstructure:"port"`
	ReadTimeout  time.Duration `mapstructure:"read_timeout"`
	WriteTimeout time.Duration `mapstructure:"write_timeout"`
	TLS          TLSConfig     `mapstructure:"tls"`
}

// TLSConfig TLS 配置
type TLSConfig struct {
	Enabled  bool   `mapstructure:"enabled"`
	CertFile string `mapstructure:"cert_file"`
	KeyFile  string `mapstructure:"key_file"`
}

// MurmurConfig Murmur 默认后端配置
type MurmurConfig struct {
	Host     string        `mapstructure:"host"`
	Port     int           `mapstructure:"port"`
	Timeout  time.Duration `mapstructure:"timeout"`
	Reconnect ReconnectConfig `mapstructure:"reconnect"`
}

// ReconnectConfig 重连配置
type ReconnectConfig struct {
	Enabled    bool          `mapstructure:"enabled"`
	MaxAttempts int          `mapstructure:"max_attempts"`
	Delay      time.Duration `mapstructure:"delay"`
}

// BackendConfig 后端服务器配置 (多租户支持)
type BackendConfig struct {
	Name   string   `mapstructure:"name"`
	Host   string   `mapstructure:"host"`
	Port   int      `mapstructure:"port"`
	Tokens []string `mapstructure:"tokens"`
}

// AuthConfig 认证配置
type AuthConfig struct {
	Enabled    bool   `mapstructure:"enabled"`
	HeaderName string `mapstructure:"header"`
	QueryParam string `mapstructure:"query_param"`
}

// MetricsConfig 指标配置
type MetricsConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	Port    int    `mapstructure:"port"`
	Path    string `mapstructure:"path"`
}

// LoggingConfig 日志配置
type LoggingConfig struct {
	Level  string `mapstructure:"level"`
	Format string `mapstructure:"format"` // json, text
}

// LimitsConfig 限制配置
type LimitsConfig struct {
	MaxConnections      int           `mapstructure:"max_connections"`
	MaxConnectionsPerIP int           `mapstructure:"max_connections_per_ip"`
	ConnectionTimeout   time.Duration `mapstructure:"connection_timeout"`
}

// DefaultConfig 返回默认配置
func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Host:         "0.0.0.0",
			Port:         8080,
			ReadTimeout:  30 * time.Second,
			WriteTimeout: 30 * time.Second,
			TLS: TLSConfig{
				Enabled: false,
			},
		},
		Murmur: MurmurConfig{
			Host:    "localhost",
			Port:    64738,
			Timeout: 10 * time.Second,
			Reconnect: ReconnectConfig{
				Enabled:     true,
				MaxAttempts: 3,
				Delay:       5 * time.Second,
			},
		},
		Backends: []BackendConfig{},
		Auth: AuthConfig{
			Enabled:    false,
			HeaderName: "X-Mumble-Token",
			QueryParam: "token",
		},
		Metrics: MetricsConfig{
			Enabled: true,
			Port:    9090,
			Path:    "/metrics",
		},
		Logging: LoggingConfig{
			Level:  "info",
			Format: "json",
		},
		Limits: LimitsConfig{
			MaxConnections:      1000,
			MaxConnectionsPerIP: 10,
			ConnectionTimeout:   300 * time.Second,
		},
	}
}

// Load 从文件加载配置
func Load(configPath string) (*Config, error) {
	cfg := DefaultConfig()

	v := viper.New()
	v.SetConfigFile(configPath)
	v.SetConfigType("yaml")

	// 环境变量支持
	v.SetEnvPrefix("MUMBLE")
	v.AutomaticEnv()

	// 读取配置文件
	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// 解析配置
	if err := v.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// 验证配置
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return cfg, nil
}

// LoadFromEnv 从环境变量加载配置
func LoadFromEnv() *Config {
	cfg := DefaultConfig()

	v := viper.New()
	v.SetEnvPrefix("MUMBLE")
	v.AutomaticEnv()

	// 绑定环境变量
	_ = v.BindEnv("server.host", "MUMBLE_SERVER_HOST")
	_ = v.BindEnv("server.port", "MUMBLE_SERVER_PORT")
	_ = v.BindEnv("murmur.host", "MUMBLE_MURMUR_HOST")
	_ = v.BindEnv("murmur.port", "MUMBLE_MURMUR_PORT")
	_ = v.BindEnv("metrics.enabled", "MUMBLE_METRICS_ENABLED")
	_ = v.BindEnv("metrics.port", "MUMBLE_METRICS_PORT")

	_ = v.Unmarshal(cfg)

	return cfg
}

// Validate 验证配置
func (c *Config) Validate() error {
	// 验证服务器配置
	if c.Server.Port < 1 || c.Server.Port > 65535 {
		return fmt.Errorf("invalid server port: %d", c.Server.Port)
	}

	// 验证 Murmur 配置
	if c.Murmur.Port < 1 || c.Murmur.Port > 65535 {
		return fmt.Errorf("invalid murmur port: %d", c.Murmur.Port)
	}

	// 验证 TLS 配置
	if c.Server.TLS.Enabled {
		if c.Server.TLS.CertFile == "" || c.Server.TLS.KeyFile == "" {
			return fmt.Errorf("TLS enabled but cert/key files not specified")
		}
	}

	// 验证日志级别
	validLevels := map[string]bool{
		"debug": true,
		"info":  true,
		"warn":  true,
		"error": true,
	}
	if !validLevels[c.Logging.Level] {
		return fmt.Errorf("invalid log level: %s", c.Logging.Level)
	}

	// 验证日志格式
	if c.Logging.Format != "json" && c.Logging.Format != "text" {
		return fmt.Errorf("invalid log format: %s", c.Logging.Format)
	}

	return nil
}

// GetDefaultBackend 获取默认后端配置
func (c *Config) GetDefaultBackend() *BackendConfig {
	return &BackendConfig{
		Name: "default",
		Host: c.Murmur.Host,
		Port: c.Murmur.Port,
	}
}