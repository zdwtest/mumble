package proxy

import (
	"net"
	"sync"
)

// IPWhitelist IP 白名单
type IPWhitelist struct {
	enabled bool
	nets    []*net.IPNet
	ips     map[string]bool
	mu      sync.RWMutex
}

// NewIPWhitelist 创建 IP 白名单
func NewIPWhitelist() *IPWhitelist {
	return &IPWhitelist{
		ips: make(map[string]bool),
	}
}

// Enable 启用白名单
func (w *IPWhitelist) Enable() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.enabled = true
}

// Disable 禁用白名单
func (w *IPWhitelist) Disable() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.enabled = false
}

// IsEnabled 检查是否启用
func (w *IPWhitelist) IsEnabled() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.enabled
}

// AddIP 添加 IP 地址
func (w *IPWhitelist) AddIP(ip string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	// 解析 IP 地址验证格式
	if net.ParseIP(ip) == nil {
		return &net.ParseError{Type: "IP address", Text: ip}
	}

	w.ips[ip] = true
	return nil
}

// RemoveIP 移除 IP 地址
func (w *IPWhitelist) RemoveIP(ip string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.ips, ip)
}

// AddCIDR 添加 CIDR 网段
func (w *IPWhitelist) AddCIDR(cidr string) error {
	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return err
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	w.nets = append(w.nets, ipNet)
	return nil
}

// RemoveCIDR 移除 CIDR 网段
func (w *IPWhitelist) RemoveCIDR(cidr string) {
	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	for i, n := range w.nets {
		if n.String() == ipNet.String() {
			w.nets = append(w.nets[:i], w.nets[i+1:]...)
			break
		}
	}
}

// Allow 检查 IP 是否在白名单中
func (w *IPWhitelist) Allow(ipStr string) bool {
	w.mu.RLock()
	defer w.mu.RUnlock()

	// 如果未启用，允许所有
	if !w.enabled {
		return true
	}

	// 提取 IP 地址（去掉端口）
	ip := net.ParseIP(ipStr)
	if ip == nil {
		// 尝试从 host:port 格式提取
		host, _, err := net.SplitHostPort(ipStr)
		if err != nil {
			return false
		}
		ip = net.ParseIP(host)
		if ip == nil {
			return false
		}
	}

	// 检查精确匹配
	if w.ips[ip.String()] {
		return true
	}

	// 检查 CIDR 网段
	for _, ipNet := range w.nets {
		if ipNet.Contains(ip) {
			return true
		}
	}

	return false
}

// List 列出所有白名单条目
func (w *IPWhitelist) List() (ips []string, cidrs []string) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	ips = make([]string, 0, len(w.ips))
	for ip := range w.ips {
		ips = append(ips, ip)
	}

	cidrs = make([]string, 0, len(w.nets))
	for _, ipNet := range w.nets {
		cidrs = append(cidrs, ipNet.String())
	}

	return
}

// Clear 清空白名单
func (w *IPWhitelist) Clear() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.ips = make(map[string]bool)
	w.nets = nil
}

// Count 获取白名单条目数量
func (w *IPWhitelist) Count() (ipCount, cidrCount int) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return len(w.ips), len(w.nets)
}

// IPBlacklist IP 黑名单
type IPBlacklist struct {
	enabled bool
	ips     map[string]bool
	nets    []*net.IPNet
	mu      sync.RWMutex
}

// NewIPBlacklist 创建 IP 黑名单
func NewIPBlacklist() *IPBlacklist {
	return &IPBlacklist{
		ips: make(map[string]bool),
	}
}

// Enable 启用黑名单
func (b *IPBlacklist) Enable() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.enabled = true
}

// Disable 禁用黑名单
func (b *IPBlacklist) Disable() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.enabled = false
}

// AddIP 添加 IP 到黑名单
func (b *IPBlacklist) AddIP(ip string) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if net.ParseIP(ip) == nil {
		return &net.ParseError{Type: "IP address", Text: ip}
	}

	b.ips[ip] = true
	return nil
}

// RemoveIP 从黑名单移除 IP
func (b *IPBlacklist) RemoveIP(ip string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.ips, ip)
}

// AddCIDR 添加 CIDR 到黑名单
func (b *IPBlacklist) AddCIDR(cidr string) error {
	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return err
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	b.nets = append(b.nets, ipNet)
	return nil
}

// Blocked 检查 IP 是否被阻止
func (b *IPBlacklist) Blocked(ipStr string) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if !b.enabled {
		return false
	}

	ip := net.ParseIP(ipStr)
	if ip == nil {
		host, _, err := net.SplitHostPort(ipStr)
		if err != nil {
			return false
		}
		ip = net.ParseIP(host)
		if ip == nil {
			return false
		}
	}

	// 检查精确匹配
	if b.ips[ip.String()] {
		return true
	}

	// 检查 CIDR
	for _, ipNet := range b.nets {
		if ipNet.Contains(ip) {
			return true
		}
	}

	return false
}

// List 列出黑名单
func (b *IPBlacklist) List() (ips []string, cidrs []string) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	ips = make([]string, 0, len(b.ips))
	for ip := range b.ips {
		ips = append(ips, ip)
	}

	cidrs = make([]string, 0, len(b.nets))
	for _, ipNet := range b.nets {
		cidrs = append(cidrs, ipNet.String())
	}

	return
}

// Clear 清空黑名单
func (b *IPBlacklist) Clear() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.ips = make(map[string]bool)
	b.nets = nil
}