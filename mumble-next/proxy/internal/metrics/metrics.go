package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics Prometheus 指标集合
type Metrics struct {
	// ActiveConnections 当前活跃连接数
	ActiveConnections prometheus.Gauge

	// TotalConnections 总连接数
	TotalConnections prometheus.Counter

	// BytesTransferred 传输字节数
	BytesTransferred *prometheus.CounterVec

	// ConnectionDuration 连接持续时间
	ConnectionDuration prometheus.Histogram

	// Errors 错误计数
	Errors *prometheus.CounterVec

	// BackendConnections 每个后端的连接数
	BackendConnections *prometheus.GaugeVec

	// registry for custom registration
	registry prometheus.Registerer
}

// NewMetrics 创建新的指标集合 (使用默认注册器)
// 注意：全局只能调用一次，多次调用会导致重复注册
func NewMetrics() *Metrics {
	return NewMetricsWithRegistry(prometheus.DefaultRegisterer)
}

// NewMetricsWithRegistry 使用自定义注册器创建指标集合
func NewMetricsWithRegistry(registry prometheus.Registerer) *Metrics {
	factory := promauto.With(registry)

	return &Metrics{
		ActiveConnections: factory.NewGauge(prometheus.GaugeOpts{
			Name: "mumble_proxy_active_connections",
			Help: "Current number of active connections",
		}),
		TotalConnections: factory.NewCounter(prometheus.CounterOpts{
			Name: "mumble_proxy_total_connections",
			Help: "Total number of connections",
		}),
		BytesTransferred: factory.NewCounterVec(
			prometheus.CounterOpts{
				Name: "mumble_proxy_bytes_transferred_total",
				Help: "Total bytes transferred",
			},
			[]string{"direction", "backend"},
		),
		ConnectionDuration: factory.NewHistogram(prometheus.HistogramOpts{
			Name:    "mumble_proxy_connection_duration_seconds",
			Help:    "Duration of connections in seconds",
			Buckets: prometheus.ExponentialBuckets(1, 2, 15), // 1s to ~9 hours
		}),
		Errors: factory.NewCounterVec(
			prometheus.CounterOpts{
				Name: "mumble_proxy_errors_total",
				Help: "Total number of errors",
			},
			[]string{"type", "backend"},
		),
		BackendConnections: factory.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "mumble_proxy_backend_connections",
				Help: "Current connections per backend",
			},
			[]string{"backend"},
		),
		registry: registry,
	}
}

// NewNoopMetrics 创建无操作指标集合 (用于测试)
// 使用 noop 注册器避免 Prometheus 注册问题
func NewNoopMetrics() *Metrics {
	return NewMetricsWithRegistry(NewNoopRegistry())
}

// NewNoopRegistry 创建无操作注册器
func NewNoopRegistry() prometheus.Registerer {
	return &noopRegistry{}
}

// IncrementConnections 增加连接计数
func (m *Metrics) IncrementConnections(backend string) {
	if m.BackendConnections != nil {
		m.ActiveConnections.Inc()
		m.TotalConnections.Inc()
		m.BackendConnections.WithLabelValues(backend).Inc()
	}
}

// DecrementConnections 减少连接计数
func (m *Metrics) DecrementConnections(backend string) {
	if m.BackendConnections != nil {
		m.ActiveConnections.Dec()
		m.BackendConnections.WithLabelValues(backend).Dec()
	}
}

// RecordBytesTransferred 记录传输字节数
func (m *Metrics) RecordBytesTransferred(direction, backend string, bytes int64) {
	if m.BytesTransferred != nil {
		m.BytesTransferred.WithLabelValues(direction, backend).Add(float64(bytes))
	}
}

// RecordConnectionDuration 记录连接持续时间
func (m *Metrics) RecordConnectionDuration(seconds float64) {
	if m.ConnectionDuration != nil {
		m.ConnectionDuration.Observe(seconds)
	}
}

// RecordError 记录错误
func (m *Metrics) RecordError(errorType, backend string) {
	if m.Errors != nil {
		m.Errors.WithLabelValues(errorType, backend).Inc()
	}
}

// =============================================================================
// Noop Registry for testing
// =============================================================================

type noopRegistry struct{}

func (noopRegistry) Register(prometheus.Collector) error  { return nil }
func (noopRegistry) MustRegister(...prometheus.Collector) {}
func (noopRegistry) Unregister(prometheus.Collector) bool { return true }