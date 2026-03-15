package health

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"
)

// Status 健康状态
type Status string

const (
	StatusHealthy   Status = "healthy"
	StatusUnhealthy Status = "unhealthy"
	StatusDegraded  Status = "degraded"
)

// Check 单项检查结果
type Check struct {
	Status  Status `json:"status"`
	Latency int64  `json:"latency_ms"`
	Error   string `json:"error,omitempty"`
}

// HealthStatus 健康检查状态
type HealthStatus struct {
	Status    Status           `json:"status"`
	Timestamp time.Time        `json:"timestamp"`
	Uptime    string           `json:"uptime"`
	Checks    map[string]Check `json:"checks"`
}

// Checker 健康检查器
type Checker struct {
	murmurHost    string
	murmurPort    int
	startTime     time.Time
	mu            sync.RWMutex
	lastStatus    *HealthStatus
	checkInterval time.Duration
}

// NewChecker 创建健康检查器
func NewChecker(murmurHost string, murmurPort int) *Checker {
	return &Checker{
		murmurHost:    murmurHost,
		murmurPort:    murmurPort,
		startTime:     time.Now(),
		checkInterval: 10 * time.Second,
	}
}

// CheckMurmur 检查 Murmur 连接
func (c *Checker) CheckMurmur() Check {
	start := time.Now()

	addr := net.JoinHostPort(c.murmurHost, fmt.Sprintf("%d", c.murmurPort))

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return Check{
			Status: StatusUnhealthy,
			Error:  err.Error(),
		}
	}
	conn.Close()

	latency := time.Since(start).Milliseconds()

	return Check{
		Status:  StatusHealthy,
		Latency: latency,
	}
}

// Check 执行所有健康检查
func (c *Checker) Check() *HealthStatus {
	c.mu.Lock()
	defer c.mu.Unlock()

	checks := make(map[string]Check)

	// 检查 Murmur
	murmurCheck := c.CheckMurmur()
	checks["murmur"] = murmurCheck

	// 确定整体状态
	overallStatus := StatusHealthy
	for _, check := range checks {
		if check.Status == StatusUnhealthy {
			overallStatus = StatusUnhealthy
			break
		}
		if check.Status == StatusDegraded {
			overallStatus = StatusDegraded
		}
	}

	status := &HealthStatus{
		Status:    overallStatus,
		Timestamp: time.Now(),
		Uptime:    time.Since(c.startTime).String(),
		Checks:    checks,
	}

	c.lastStatus = status
	return status
}

// GetLastStatus 获取上次检查状态
func (c *Checker) GetLastStatus() *HealthStatus {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lastStatus
}

// Handler 返回 HTTP 处理器
func (c *Checker) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		status := c.Check()

		w.Header().Set("Content-Type", "application/json")

		if status.Status == StatusUnhealthy {
			w.WriteHeader(http.StatusServiceUnavailable)
		} else {
			w.WriteHeader(http.StatusOK)
		}

		json.NewEncoder(w).Encode(status)
	}
}

// LivenessHandler 存活检查处理器
func (c *Checker) LivenessHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}
}

// ReadinessHandler 就绪检查处理器
func (c *Checker) ReadinessHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		status := c.Check()

		if status.Status == StatusHealthy {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("Ready"))
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte("Not Ready"))
		}
	}
}