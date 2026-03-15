package metrics

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestNewMetrics(t *testing.T) {
	// Create a new registry to avoid conflicts with global registry
	registry := prometheus.NewRegistry()
	m := createMetricsWithRegistry(registry)
	if m == nil {
		t.Fatal("expected metrics to be created")
	}
}

// createMetricsWithRegistry creates metrics with a custom registry to avoid conflicts
func createMetricsWithRegistry(registry *prometheus.Registry) *Metrics {
	return &Metrics{
		ActiveConnections: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "test_active_connections",
			Help: "test",
		}),
		TotalConnections: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "test_total_connections",
			Help: "test",
		}),
		BytesTransferred: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "test_bytes_transferred_total",
				Help: "test",
			},
			[]string{"direction", "backend"},
		),
		ConnectionDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "test_connection_duration_seconds",
			Help:    "test",
			Buckets: prometheus.ExponentialBuckets(1, 2, 15),
		}),
		Errors: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "test_errors_total",
				Help: "test",
			},
			[]string{"type", "backend"},
		),
		BackendConnections: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "test_backend_connections",
				Help: "test",
			},
			[]string{"backend"},
		),
	}
}

func TestMetrics_IncrementDecrementConnections(t *testing.T) {
	registry := prometheus.NewRegistry()
	m := createMetricsWithRegistry(registry)
	registry.MustRegister(
		m.ActiveConnections,
		m.TotalConnections,
		m.BackendConnections,
	)

	m.IncrementConnections("default")

	// 验证活跃连接数
	if testutil.ToFloat64(m.ActiveConnections) != 1 {
		t.Errorf("expected active connections to be 1, got %v", testutil.ToFloat64(m.ActiveConnections))
	}

	m.IncrementConnections("default")

	if testutil.ToFloat64(m.ActiveConnections) != 2 {
		t.Errorf("expected active connections to be 2, got %v", testutil.ToFloat64(m.ActiveConnections))
	}

	m.DecrementConnections("default")

	if testutil.ToFloat64(m.ActiveConnections) != 1 {
		t.Errorf("expected active connections to be 1 after decrement, got %v", testutil.ToFloat64(m.ActiveConnections))
	}
}

func TestMetrics_BytesTransferred(t *testing.T) {
	registry := prometheus.NewRegistry()
	m := createMetricsWithRegistry(registry)
	registry.MustRegister(m.BytesTransferred)

	m.RecordBytesTransferred("sent", "default", 1024)
	m.RecordBytesTransferred("received", "default", 2048)

	// 验证指标被记录
	metricFamilies, err := registry.Gather()
	if err != nil {
		t.Fatalf("failed to gather metrics: %v", err)
	}

	// 找到 bytes_transferred 指标
	found := false
	for _, mf := range metricFamilies {
		if strings.Contains(mf.GetName(), "bytes_transferred") {
			found = true
			break
		}
	}

	if !found {
		t.Error("expected to find bytes_transferred metric")
	}
}

func TestMetrics_RecordError(t *testing.T) {
	registry := prometheus.NewRegistry()
	m := createMetricsWithRegistry(registry)
	registry.MustRegister(m.Errors)

	m.RecordError("connection_failed", "default")
	m.RecordError("websocket_upgrade", "default")

	// 验证错误指标被记录
	metricFamilies, err := registry.Gather()
	if err != nil {
		t.Fatalf("failed to gather metrics: %v", err)
	}

	found := false
	for _, mf := range metricFamilies {
		if strings.Contains(mf.GetName(), "errors") {
			found = true
			break
		}
	}

	if !found {
		t.Error("expected to find errors metric")
	}
}

func TestMetrics_RecordConnectionDuration(t *testing.T) {
	registry := prometheus.NewRegistry()
	m := createMetricsWithRegistry(registry)
	registry.MustRegister(m.ConnectionDuration)

	// 记录几个不同时长的连接
	m.RecordConnectionDuration(1.5)
	m.RecordConnectionDuration(10.0)
	m.RecordConnectionDuration(60.0)

	// 验证直方图指标存在
	metricFamilies, err := registry.Gather()
	if err != nil {
		t.Fatalf("failed to gather metrics: %v", err)
	}

	found := false
	for _, mf := range metricFamilies {
		if strings.Contains(mf.GetName(), "connection_duration") {
			found = true
			break
		}
	}

	if !found {
		t.Error("expected to find connection_duration metric")
	}
}

func TestNewNoopMetrics(t *testing.T) {
	m := NewNoopMetrics()
	if m == nil {
		t.Fatal("expected noop metrics to be created")
	}

	// Test that all operations work without panic
	m.IncrementConnections("test")
	m.DecrementConnections("test")
	m.RecordBytesTransferred("sent", "test", 1024)
	m.RecordConnectionDuration(1.5)
	m.RecordError("test_error", "test")
}

func TestNewNoopRegistry(t *testing.T) {
	registry := NewNoopRegistry()
	if registry == nil {
		t.Fatal("expected noop registry to be created")
	}

	// Test that all operations work without panic
	err := registry.Register(prometheus.NewGauge(prometheus.GaugeOpts{Name: "test", Help: "test"}))
	if err != nil {
		t.Errorf("expected Register to return nil, got %v", err)
	}

	registry.MustRegister()
	result := registry.Unregister(prometheus.NewGauge(prometheus.GaugeOpts{Name: "test", Help: "test"}))
	if !result {
		t.Error("expected Unregister to return true")
	}
}

func TestNewMetricsWithRegistry(t *testing.T) {
	registry := prometheus.NewRegistry()
	m := NewMetricsWithRegistry(registry)
	if m == nil {
		t.Fatal("expected metrics to be created")
	}

	// Test that metrics work
	m.IncrementConnections("backend1")
	m.DecrementConnections("backend1")
	m.RecordBytesTransferred("sent", "backend1", 100)
	m.RecordConnectionDuration(1.0)
	m.RecordError("test_type", "backend1")
}