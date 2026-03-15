package audit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if !cfg.Enabled {
		t.Error("expected audit to be enabled by default")
	}

	if cfg.FilePath == "" {
		t.Error("expected file path to be set")
	}

	if cfg.Format != "json" {
		t.Errorf("expected json format, got %s", cfg.Format)
	}
}

func TestNewLogger_Disabled(t *testing.T) {
	cfg := Config{
		Enabled: false,
	}

	logger, err := NewLogger(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should not error when logging while disabled
	logger.Log(Event{Type: EventConnect, Action: "test"})
	logger.LogConnect("192.168.1.1", "backend1", "client1")

	if logger.IsEnabled() {
		t.Error("expected logger to be disabled")
	}

	logger.Close()
}

func TestNewLogger_Enabled(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "audit-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := Config{
		Enabled:       true,
		FilePath:      filepath.Join(tmpDir, "audit.log"),
		Format:        "json",
		BufferSize:    10,
		FlushInterval: time.Second,
	}

	logger, err := NewLogger(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer logger.Close()

	// Log some events
	logger.LogConnect("192.168.1.1", "backend1", "client1")
	logger.LogDisconnect("192.168.1.1", "backend1", "client1", 5*time.Second)
	logger.LogAuthSuccess("192.168.1.1", "backend1", "abcd")

	// Flush to write to file
	if err := logger.Flush(); err != nil {
		t.Errorf("flush error: %v", err)
	}

	// Check file exists and has content
	data, err := os.ReadFile(cfg.FilePath)
	if err != nil {
		t.Fatalf("failed to read audit file: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "connect") {
		t.Error("expected connect event in log")
	}
	if !strings.Contains(content, "disconnect") {
		t.Error("expected disconnect event in log")
	}
	if !strings.Contains(content, "auth_success") {
		t.Error("expected auth_success event in log")
	}
}

func TestLogger_LogMethods(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "audit-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := Config{
		Enabled:       true,
		FilePath:      filepath.Join(tmpDir, "audit.log"),
		Format:        "json",
		BufferSize:    1, // Small buffer to trigger flush
		FlushInterval: time.Second,
	}

	logger, err := NewLogger(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer logger.Close()

	// Test all log methods
	logger.LogAuthFailure("192.168.1.2", "invalid token")
	logger.LogBackendCreate("backend2", "localhost", 64738)
	logger.LogBackendDelete("backend2")
	logger.LogIPBlocked("10.0.0.1", "blacklisted")
	logger.LogRateLimited("192.168.1.3", 15, 10)
	logger.LogError(os.ErrClosed, map[string]interface{}{"context": "test"})
	logger.LogConfigReload(true, []string{"murmur.host"})
	logger.LogShutdown("maintenance")

	logger.Flush()

	// Verify file has all events
	data, err := os.ReadFile(cfg.FilePath)
	if err != nil {
		t.Fatalf("failed to read audit file: %v", err)
	}

	content := string(data)
	expectedEvents := []string{
		"auth_failure",
		"backend_create",
		"backend_delete",
		"ip_blocked",
		"rate_limited",
		"error",
		"config_reload",
		"shutdown",
	}

	for _, event := range expectedEvents {
		if !strings.Contains(content, event) {
			t.Errorf("expected event %s in log", event)
		}
	}
}

func TestLogger_TextFormat(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "audit-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := Config{
		Enabled:       true,
		FilePath:      filepath.Join(tmpDir, "audit.log"),
		Format:        "text",
		BufferSize:    10,
		FlushInterval: time.Second,
	}

	logger, err := NewLogger(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer logger.Close()

	logger.LogConnect("192.168.1.1", "backend1", "client1")
	logger.Flush()

	data, err := os.ReadFile(cfg.FilePath)
	if err != nil {
		t.Fatalf("failed to read audit file: %v", err)
	}

	content := string(data)
	// Text format should have readable content
	if !strings.Contains(content, "connect") {
		t.Error("expected connect in text format")
	}
	if !strings.Contains(content, "client=192.168.1.1") {
		t.Error("expected client IP in text format")
	}
}

func TestLogger_EnableDisable(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "audit-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := Config{
		Enabled:       true,
		FilePath:      filepath.Join(tmpDir, "audit.log"),
		Format:        "json",
		BufferSize:    10,
		FlushInterval: time.Second,
	}

	logger, err := NewLogger(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer logger.Close()

	// Log while enabled
	logger.LogConnect("192.168.1.1", "backend1", "client1")

	// Disable
	logger.Disable()
	if logger.IsEnabled() {
		t.Error("expected logger to be disabled")
	}

	// Log while disabled
	logger.LogConnect("192.168.1.2", "backend1", "client2")

	// Enable again
	logger.Enable()
	logger.LogConnect("192.168.1.3", "backend1", "client3")

	logger.Flush()

	data, err := os.ReadFile(cfg.FilePath)
	if err != nil {
		t.Fatalf("failed to read audit file: %v", err)
	}

	content := string(data)
	// Should have client1 and client3, but not client2
	if !strings.Contains(content, "client1") {
		t.Error("expected client1 in log")
	}
	if strings.Contains(content, "client2") {
		t.Error("should not have client2 in log (logged while disabled)")
	}
	if !strings.Contains(content, "client3") {
		t.Error("expected client3 in log")
	}
}

func TestLogger_Stats(t *testing.T) {
	cfg := Config{Enabled: false}
	logger, err := NewLogger(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer logger.Close()

	// Log some events (won't actually log since disabled)
	logger.LogConnect("192.168.1.1", "backend1", "client1")
	logger.LogDisconnect("192.168.1.1", "backend1", "client1", time.Second)

	events, errors := logger.Stats()
	// Stats might be 0 since we're not actually writing
	t.Logf("Stats: events=%d, errors=%d", events, errors)
}

func TestLogger_Rotate(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "audit-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := Config{
		Enabled:       true,
		FilePath:      filepath.Join(tmpDir, "audit.log"),
		Format:        "json",
		BufferSize:    10,
		FlushInterval: time.Second,
	}

	logger, err := NewLogger(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer logger.Close()

	// Log before rotate
	logger.LogConnect("192.168.1.1", "backend1", "client1")
	logger.Flush()

	// Rotate
	if err := logger.Rotate(); err != nil {
		t.Errorf("rotate error: %v", err)
	}

	// Log after rotate
	logger.LogConnect("192.168.1.2", "backend1", "client2")
	logger.Flush()

	// Check that backup file exists
	files, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatalf("failed to read dir: %v", err)
	}

	// Should have audit.log and a backup
	if len(files) < 2 {
		t.Errorf("expected at least 2 files, got %d", len(files))
	}
}

func TestNoopLogger(t *testing.T) {
	logger := NewNoopLogger()

	// All methods should not panic
	logger.Log(Event{Type: EventConnect})
	logger.LogConnect("192.168.1.1", "backend1", "client1")
	logger.LogDisconnect("192.168.1.1", "backend1", "client1", time.Second)
	logger.LogAuthSuccess("192.168.1.1", "backend1", "abcd")
	logger.LogAuthFailure("192.168.1.1", "test")
	logger.LogBackendCreate("backend1", "localhost", 64738)
	logger.LogBackendDelete("backend1")
	logger.LogIPBlocked("192.168.1.1", "test")
	logger.LogRateLimited("192.168.1.1", 10, 5)
	logger.LogError(os.ErrClosed, nil)
	logger.LogConfigReload(true, nil)
	logger.LogShutdown("test")

	if logger.IsEnabled() {
		t.Error("expected noop logger to be disabled")
	}

	events, errors := logger.Stats()
	if events != 0 || errors != 0 {
		t.Error("expected noop logger to have 0 stats")
	}

	if err := logger.Flush(); err != nil {
		t.Errorf("expected flush to succeed: %v", err)
	}

	if err := logger.Close(); err != nil {
		t.Errorf("expected close to succeed: %v", err)
	}

	if err := logger.Rotate(); err != nil {
		t.Errorf("expected rotate to succeed: %v", err)
	}
}

func TestEvent_Timestamp(t *testing.T) {
	cfg := Config{Enabled: false}
	logger, err := NewLogger(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer logger.Close()

	// Event without timestamp
	event := Event{Type: EventConnect, Action: "test"}
	logger.Log(event)

	// Event with timestamp
	event2 := Event{
		Type:      EventConnect,
		Action:    "test2",
		Timestamp: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	logger.Log(event2)

	// No error means success
}

func TestFormatText(t *testing.T) {
	event := Event{
		Timestamp: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
		Type:      EventConnect,
		Action:    "client connected",
		ClientIP:  "192.168.1.1",
		Backend:   "backend1",
		Resource:  "client123",
		Result:    "success",
		Duration:  5 * time.Second,
	}

	text := formatText(event)

	expectedParts := []string{
		"2024-01-01T12:00:00Z",
		"connect",
		"client connected",
		"client=192.168.1.1",
		"backend=backend1",
		"resource=client123",
		"result=success",
		"duration=5s",
	}

	for _, part := range expectedParts {
		if !strings.Contains(text, part) {
			t.Errorf("expected text to contain %q", part)
		}
	}
}

func TestLogger_BufferFlush(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "audit-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := Config{
		Enabled:       true,
		FilePath:      filepath.Join(tmpDir, "audit.log"),
		Format:        "json",
		BufferSize:    3, // Small buffer
		FlushInterval: time.Minute, // Long interval so we test buffer flush
	}

	logger, err := NewLogger(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer logger.Close()

	// Log up to buffer size
	logger.LogConnect("192.168.1.1", "backend1", "client1")
	logger.LogConnect("192.168.1.2", "backend1", "client2")
	logger.LogConnect("192.168.1.3", "backend1", "client3")

	// Wait for buffer flush
	time.Sleep(100 * time.Millisecond)

	// File should exist and have content
	data, err := os.ReadFile(cfg.FilePath)
	if err != nil {
		t.Fatalf("failed to read audit file: %v", err)
	}

	if len(data) == 0 {
		t.Error("expected file to have content after buffer flush")
	}
}