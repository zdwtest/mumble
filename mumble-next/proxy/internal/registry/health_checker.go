package registry

import (
	"net"
	"strconv"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/mumble/mumble-next/proxy/internal/discovery"
)

// HealthChecker 健康检查器
type HealthChecker struct {
	discovery discovery.Discovery
	logger    *zap.Logger

	instances map[string]*discovery.Instance
	mu        sync.RWMutex

	stopCh chan struct{}
	running bool
}

// NewHealthChecker 创建健康检查器
func NewHealthChecker(d discovery.Discovery, logger *zap.Logger) *HealthChecker {
	return &HealthChecker{
		discovery: d,
		logger:    logger,
		instances: make(map[string]*discovery.Instance),
	}
}

// Start 启动健康检查
func (h *HealthChecker) Start(interval time.Duration) {
	h.mu.Lock()
	if h.running {
		h.mu.Unlock()
		return
	}
	h.running = true
	h.stopCh = make(chan struct{})
	h.mu.Unlock()

	go h.run(interval)
}

// Stop 停止健康检查
func (h *HealthChecker) Stop() {
	h.mu.Lock()
	defer h.mu.Unlock()

	if !h.running {
		return
	}

	h.running = false
	close(h.stopCh)
}

// run 运行健康检查循环
func (h *HealthChecker) run(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			h.checkAll()
		case <-h.stopCh:
			return
		}
	}
}

// checkAll 检查所有实例
func (h *HealthChecker) checkAll() {
	h.mu.RLock()
	instances := make([]*discovery.Instance, 0, len(h.instances))
	for _, inst := range h.instances {
		instances = append(instances, inst)
	}
	h.mu.RUnlock()

	for _, inst := range instances {
		healthy := h.checkInstance(inst)
		inst.Healthy = healthy
		inst.UpdatedAt = time.Now()
	}
}

// checkInstance 检查单个实例
func (h *HealthChecker) checkInstance(inst *discovery.Instance) bool {
	addr := net.JoinHostPort(inst.Host, strconv.Itoa(inst.Port))
	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		h.logger.Debug("health check failed",
			zap.String("instance", inst.ID),
			zap.Error(err),
		)
		return false
	}
	conn.Close()

	h.logger.Debug("health check passed",
		zap.String("instance", inst.ID),
	)
	return true
}

// AddInstance 添加实例到健康检查
func (h *HealthChecker) AddInstance(inst *discovery.Instance) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.instances[inst.ID] = inst
}

// RemoveInstance 从健康检查移除实例
func (h *HealthChecker) RemoveInstance(instanceID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.instances, instanceID)
}

// Check 手动检查实例健康状态
func (h *HealthChecker) Check(inst *discovery.Instance) error {
	addr := net.JoinHostPort(inst.Host, strconv.Itoa(inst.Port))
	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		return err
	}
	conn.Close()
	return nil
}