package admin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestLogStreamer_WriteLog(t *testing.T) {
	logger := zap.NewNop()
	ls := NewLogStreamer(100, logger)
	defer ls.Close()

	entry := &LogEntry{
		Level:   "info",
		Message: "test message",
		Source:  "test",
	}

	ls.WriteLog(entry)

	// Wait for async processing
	time.Sleep(50 * time.Millisecond)

	buffer := ls.GetBuffer()
	if len(buffer) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(buffer))
	}

	if buffer[0].Message != "test message" {
		t.Errorf("expected message 'test message', got %s", buffer[0].Message)
	}
	if buffer[0].Timestamp.IsZero() {
		t.Error("expected timestamp to be set")
	}
}

func TestLogStreamer_BufferLimit(t *testing.T) {
	logger := zap.NewNop()
	ls := NewLogStreamer(10, logger) // Small buffer
	defer ls.Close()

	// Write more than buffer size
	for i := 0; i < 15; i++ {
		ls.WriteLog(&LogEntry{
			Level:   "info",
			Message: "test",
		})
	}

	// Wait for async processing
	time.Sleep(100 * time.Millisecond)

	buffer := ls.GetBuffer()
	if len(buffer) != 10 {
		t.Errorf("expected buffer size 10, got %d", len(buffer))
	}
}

func TestLogStreamer_Write(t *testing.T) {
	logger := zap.NewNop()
	ls := NewLogStreamer(100, logger)
	defer ls.Close()

	n, err := ls.Write([]byte("hello world"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 11 {
		t.Errorf("expected 11 bytes written, got %d", n)
	}

	// Wait for async processing
	time.Sleep(50 * time.Millisecond)

	buffer := ls.GetBuffer()
	if len(buffer) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(buffer))
	}
}

func TestLogStreamer_ClientCount(t *testing.T) {
	logger := zap.NewNop()
	ls := NewLogStreamer(100, logger)
	defer ls.Close()

	if ls.ClientCount() != 0 {
		t.Error("expected 0 clients initially")
	}
}

func TestLogEntry_JSON(t *testing.T) {
	entry := &LogEntry{
		Timestamp: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
		Level:     "error",
		Message:   "something went wrong",
		Source:    "handler",
		Fields: map[string]interface{}{
			"code":    500,
			"request": "req-123",
		},
	}

	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded LogEntry
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.Level != "error" {
		t.Errorf("expected level 'error', got %s", decoded.Level)
	}
	if decoded.Message != "something went wrong" {
		t.Errorf("expected message 'something went wrong', got %s", decoded.Message)
	}
}

func TestLogStreamHandler_Buffer(t *testing.T) {
	logger := zap.NewNop()
	ls := NewLogStreamer(100, logger)
	defer ls.Close()

	ls.WriteLog(&LogEntry{Level: "info", Message: "test 1"})
	ls.WriteLog(&LogEntry{Level: "warn", Message: "test 2"})

	// Wait for async processing
	time.Sleep(50 * time.Millisecond)

	h := NewLogStreamHandler(ls, logger)

	req := httptest.NewRequest("GET", "/api/logs", nil)
	rec := httptest.NewRecorder()

	h.handleBuffer(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if result["count"].(float64) != 2 {
		t.Errorf("expected count 2, got %v", result["count"])
	}
}

func TestLogStreamHandler_MethodNotAllowed(t *testing.T) {
	logger := zap.NewNop()
	ls := NewLogStreamer(100, logger)
	defer ls.Close()

	h := NewLogStreamHandler(ls, logger)

	req := httptest.NewRequest("POST", "/api/logs", bytes.NewReader(nil))
	rec := httptest.NewRecorder()

	h.handleBuffer(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", rec.Code)
	}
}

func TestZapLogBridge(t *testing.T) {
	logger := zap.NewNop()
	ls := NewLogStreamer(100, logger)
	defer ls.Close()

	bridge := NewZapLogBridge(ls, "warn", "test-module")

	n, err := bridge.Write([]byte("warning message"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 15 {
		t.Errorf("expected 15 bytes, got %d", n)
	}

	// Wait for async processing
	time.Sleep(50 * time.Millisecond)

	buffer := ls.GetBuffer()
	if len(buffer) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(buffer))
	}

	if buffer[0].Level != "warn" {
		t.Errorf("expected level 'warn', got %s", buffer[0].Level)
	}
	if buffer[0].Source != "test-module" {
		t.Errorf("expected source 'test-module', got %s", buffer[0].Source)
	}
}

func TestZapLogBridge_String(t *testing.T) {
	logger := zap.NewNop()
	ls := NewLogStreamer(100, logger)
	defer ls.Close()

	bridge := NewZapLogBridge(ls, "info", "test")
	s := bridge.String()

	if !strings.Contains(s, "info") || !strings.Contains(s, "test") {
		t.Errorf("unexpected string representation: %s", s)
	}
}

func TestParseStringSlice(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"", nil},
		{`["a","b","c"]`, []string{"a", "b", "c"}},
		{"a,b,c", []string{"a", "b", "c"}},
		{"single", []string{"single"}},
	}

	for _, tt := range tests {
		result := parseStringSlice(tt.input)

		if len(result) != len(tt.expected) {
			t.Errorf("parseStringSlice(%q): expected %v, got %v", tt.input, tt.expected, result)
			continue
		}

		for i, v := range result {
			if v != tt.expected[i] {
				t.Errorf("parseStringSlice(%q)[%d]: expected %q, got %q", tt.input, i, tt.expected[i], v)
			}
		}
	}
}