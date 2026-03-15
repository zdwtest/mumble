package proxy

import (
	"net"
	"testing"
)

func TestIPWhitelist_New(t *testing.T) {
	w := NewIPWhitelist()
	if w == nil {
		t.Fatal("expected whitelist to be created")
	}

	// Should allow all when disabled
	if !w.Allow("192.168.1.1") {
		t.Error("expected to allow all IPs when disabled")
	}
}

func TestIPWhitelist_EnableDisable(t *testing.T) {
	w := NewIPWhitelist()

	w.AddIP("192.168.1.1")

	// Still allowed when disabled
	if !w.Allow("192.168.1.2") {
		t.Error("expected to allow when disabled")
	}

	// Enable
	w.Enable()

	// Should block non-whitelisted IP
	if w.Allow("192.168.1.2") {
		t.Error("expected to block non-whitelisted IP")
	}

	// Should allow whitelisted IP
	if !w.Allow("192.168.1.1") {
		t.Error("expected to allow whitelisted IP")
	}

	// Disable
	w.Disable()

	// Should allow all again
	if !w.Allow("192.168.1.2") {
		t.Error("expected to allow all when disabled")
	}
}

func TestIPWhitelist_AddIP(t *testing.T) {
	w := NewIPWhitelist()
	w.Enable()

	// Add valid IP
	err := w.AddIP("192.168.1.1")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if !w.Allow("192.168.1.1") {
		t.Error("expected to allow added IP")
	}

	// Add invalid IP
	err = w.AddIP("invalid-ip")
	if err == nil {
		t.Error("expected error for invalid IP")
	}
}

func TestIPWhitelist_RemoveIP(t *testing.T) {
	w := NewIPWhitelist()
	w.Enable()

	w.AddIP("192.168.1.1")
	if !w.Allow("192.168.1.1") {
		t.Error("expected to allow added IP")
	}

	w.RemoveIP("192.168.1.1")
	if w.Allow("192.168.1.1") {
		t.Error("expected to block removed IP")
	}
}

func TestIPWhitelist_AddCIDR(t *testing.T) {
	w := NewIPWhitelist()
	w.Enable()

	err := w.AddCIDR("192.168.1.0/24")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Should allow IPs in the CIDR range
	testCases := []string{
		"192.168.1.1",
		"192.168.1.100",
		"192.168.1.254",
	}

	for _, ip := range testCases {
		if !w.Allow(ip) {
			t.Errorf("expected to allow IP %s", ip)
		}
	}

	// Should block IPs outside the range
	if w.Allow("192.168.2.1") {
		t.Error("expected to block IP outside CIDR")
	}
}

func TestIPWhitelist_RemoveCIDR(t *testing.T) {
	w := NewIPWhitelist()
	w.Enable()

	w.AddCIDR("192.168.1.0/24")
	if !w.Allow("192.168.1.1") {
		t.Error("expected to allow IP in CIDR")
	}

	w.RemoveCIDR("192.168.1.0/24")
	if w.Allow("192.168.1.1") {
		t.Error("expected to block IP after CIDR removal")
	}
}

func TestIPWhitelist_List(t *testing.T) {
	w := NewIPWhitelist()

	w.AddIP("192.168.1.1")
	w.AddIP("192.168.1.2")
	w.AddCIDR("10.0.0.0/8")

	ips, cidrs := w.List()

	if len(ips) != 2 {
		t.Errorf("expected 2 IPs, got %d", len(ips))
	}

	if len(cidrs) != 1 {
		t.Errorf("expected 1 CIDR, got %d", len(cidrs))
	}
}

func TestIPWhitelist_Clear(t *testing.T) {
	w := NewIPWhitelist()

	w.AddIP("192.168.1.1")
	w.AddCIDR("10.0.0.0/8")

	w.Clear()

	ipCount, cidrCount := w.Count()
	if ipCount != 0 || cidrCount != 0 {
		t.Error("expected whitelist to be empty after clear")
	}
}

func TestIPWhitelist_IPWithPort(t *testing.T) {
	w := NewIPWhitelist()
	w.Enable()

	w.AddIP("192.168.1.1")

	// Test with port
	if !w.Allow("192.168.1.1:8080") {
		t.Error("expected to allow IP with port")
	}

	// Test invalid format
	if w.Allow("invalid:format:too:many") {
		t.Error("expected to block invalid format")
	}
}

func TestIPBlacklist_New(t *testing.T) {
	b := NewIPBlacklist()
	if b == nil {
		t.Fatal("expected blacklist to be created")
	}

	// Should not block when disabled
	if b.Blocked("192.168.1.1") {
		t.Error("expected not to block when disabled")
	}
}

func TestIPBlacklist_EnableDisable(t *testing.T) {
	b := NewIPBlacklist()

	b.AddIP("192.168.1.1")

	// Not blocked when disabled
	if b.Blocked("192.168.1.1") {
		t.Error("expected not to block when disabled")
	}

	// Enable
	b.Enable()

	// Should block blacklisted IP
	if !b.Blocked("192.168.1.1") {
		t.Error("expected to block blacklisted IP")
	}

	// Should not block non-blacklisted IP
	if b.Blocked("192.168.1.2") {
		t.Error("expected not to block non-blacklisted IP")
	}
}

func TestIPBlacklist_AddRemoveIP(t *testing.T) {
	b := NewIPBlacklist()
	b.Enable()

	err := b.AddIP("192.168.1.1")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if !b.Blocked("192.168.1.1") {
		t.Error("expected to block added IP")
	}

	b.RemoveIP("192.168.1.1")
	if b.Blocked("192.168.1.1") {
		t.Error("expected not to block removed IP")
	}
}

func TestIPBlacklist_AddCIDR(t *testing.T) {
	b := NewIPBlacklist()
	b.Enable()

	b.AddCIDR("10.0.0.0/8")

	// Should block IPs in range
	if !b.Blocked("10.0.0.1") {
		t.Error("expected to block IP in CIDR")
	}

	// Should not block IPs outside range
	if b.Blocked("192.168.1.1") {
		t.Error("expected not to block IP outside CIDR")
	}
}

func TestIPBlacklist_List(t *testing.T) {
	b := NewIPBlacklist()

	b.AddIP("192.168.1.1")
	b.AddCIDR("10.0.0.0/8")

	ips, cidrs := b.List()

	if len(ips) != 1 {
		t.Errorf("expected 1 IP, got %d", len(ips))
	}

	if len(cidrs) != 1 {
		t.Errorf("expected 1 CIDR, got %d", len(cidrs))
	}
}

func TestIPBlacklist_Clear(t *testing.T) {
	b := NewIPBlacklist()

	b.AddIP("192.168.1.1")
	b.AddCIDR("10.0.0.0/8")

	b.Clear()

	ips, cidrs := b.List()
	if len(ips) != 0 || len(cidrs) != 0 {
		t.Error("expected blacklist to be empty after clear")
	}
}

func TestIPBlacklist_InvalidIP(t *testing.T) {
	b := NewIPBlacklist()

	err := b.AddIP("invalid-ip")
	if err == nil {
		t.Error("expected error for invalid IP")
	}

	var parseErr *net.ParseError
	if !errorAs(err, &parseErr) {
		t.Errorf("expected ParseError, got %T", err)
	}
}

// Helper to check error type
func errorAs[T error](err error, target *T) bool {
	return errorAsHelper(err, target)
}

func errorAsHelper[T error](err error, target *T) bool {
	if err == nil {
		return false
	}
	if e, ok := err.(T); ok {
		*target = e
		return true
	}
	return false
}