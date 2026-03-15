package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestConnectionManager_AddRemove(t *testing.T) {
	m := NewConnectionManager()

	details := &ConnectionDetails{
		ID:        "conn-1",
		ClientIP:  "192.168.1.1",
		Backend:   "default",
		UserID:    "user-1",
		State:     "active",
	}

	m.AddConnection(details)

	if m.Count() != 1 {
		t.Errorf("expected 1 connection, got %d", m.Count())
	}

	retrieved := m.GetConnection("conn-1")
	if retrieved == nil {
		t.Fatal("expected to retrieve connection")
	}

	if retrieved.ClientIP != "192.168.1.1" {
		t.Errorf("expected client IP 192.168.1.1, got %s", retrieved.ClientIP)
	}

	m.RemoveConnection("conn-1")

	if m.Count() != 0 {
		t.Errorf("expected 0 connections after remove, got %d", m.Count())
	}

	// Remove non-existent should not panic
	m.RemoveConnection("non-existent")
}

func TestConnectionManager_Update(t *testing.T) {
	m := NewConnectionManager()

	details := &ConnectionDetails{
		ID:       "conn-1",
		Backend:  "default",
		State:    "active",
	}
	m.AddConnection(details)

	// Update connection
	m.UpdateConnection("conn-1", func(d *ConnectionDetails) {
		d.BytesSent = 1000
		d.State = "closing"
	})

	retrieved := m.GetConnection("conn-1")
	if retrieved.BytesSent != 1000 {
		t.Errorf("expected bytes sent 1000, got %d", retrieved.BytesSent)
	}
	if retrieved.State != "closing" {
		t.Errorf("expected state closing, got %s", retrieved.State)
	}
}

func TestConnectionManager_AddBytes(t *testing.T) {
	m := NewConnectionManager()

	details := &ConnectionDetails{
		ID:      "conn-1",
		Backend: "default",
	}
	m.AddConnection(details)

	m.AddBytes("conn-1", 100, 200)
	m.AddBytes("conn-1", 50, 75)

	retrieved := m.GetConnection("conn-1")
	if retrieved.BytesSent != 150 {
		t.Errorf("expected bytes sent 150, got %d", retrieved.BytesSent)
	}
	if retrieved.BytesReceived != 275 {
		t.Errorf("expected bytes received 275, got %d", retrieved.BytesReceived)
	}
	if retrieved.MessagesSent != 2 {
		t.Errorf("expected 2 messages sent, got %d", retrieved.MessagesSent)
	}
	if retrieved.MessagesReceived != 2 {
		t.Errorf("expected 2 messages received, got %d", retrieved.MessagesReceived)
	}
}

func TestConnectionManager_List(t *testing.T) {
	m := NewConnectionManager()

	// Add multiple connections
	for i := 0; i < 5; i++ {
		m.AddConnection(&ConnectionDetails{
			ID:       string(rune('a' + i)),
			Backend:  "backend-1",
			State:    "active",
		})
	}
	for i := 0; i < 3; i++ {
		m.AddConnection(&ConnectionDetails{
			ID:       string(rune('f' + i)),
			Backend:  "backend-2",
			State:    "idle",
		})
	}

	// List all
	result := m.ListConnections(ConnectionListParams{})
	if result.Total != 8 {
		t.Errorf("expected total 8, got %d", result.Total)
	}

	// Filter by backend
	result = m.ListConnections(ConnectionListParams{Backend: "backend-1"})
	if result.Total != 5 {
		t.Errorf("expected 5 connections for backend-1, got %d", result.Total)
	}

	// Filter by state
	result = m.ListConnections(ConnectionListParams{State: "idle"})
	if result.Total != 3 {
		t.Errorf("expected 3 idle connections, got %d", result.Total)
	}

	// Pagination
	result = m.ListConnections(ConnectionListParams{Limit: 3, Offset: 2})
	if len(result.Connections) != 3 {
		t.Errorf("expected 3 connections, got %d", len(result.Connections))
	}
	if result.Offset != 2 {
		t.Errorf("expected offset 2, got %d", result.Offset)
	}
}

func TestConnectionManager_CountByBackend(t *testing.T) {
	m := NewConnectionManager()

	m.AddConnection(&ConnectionDetails{ID: "1", Backend: "b1"})
	m.AddConnection(&ConnectionDetails{ID: "2", Backend: "b1"})
	m.AddConnection(&ConnectionDetails{ID: "3", Backend: "b2"})

	counts := m.CountByBackend()
	if counts["b1"] != 2 {
		t.Errorf("expected 2 connections for b1, got %d", counts["b1"])
	}
	if counts["b2"] != 1 {
		t.Errorf("expected 1 connection for b2, got %d", counts["b2"])
	}
}

func TestConnectionManager_CountByState(t *testing.T) {
	m := NewConnectionManager()

	m.AddConnection(&ConnectionDetails{ID: "1", State: "active"})
	m.AddConnection(&ConnectionDetails{ID: "2", State: "active"})
	m.AddConnection(&ConnectionDetails{ID: "3", State: "idle"})

	counts := m.CountByState()
	if counts["active"] != 2 {
		t.Errorf("expected 2 active connections, got %d", counts["active"])
	}
	if counts["idle"] != 1 {
		t.Errorf("expected 1 idle connection, got %d", counts["idle"])
	}
}

func TestConnectionManager_Clear(t *testing.T) {
	m := NewConnectionManager()

	m.AddConnection(&ConnectionDetails{ID: "1"})
	m.AddConnection(&ConnectionDetails{ID: "2"})

	if m.Count() != 2 {
		t.Fatal("expected 2 connections before clear")
	}

	m.Clear()

	if m.Count() != 0 {
		t.Errorf("expected 0 connections after clear, got %d", m.Count())
	}
}

func TestConnectionHandler_List(t *testing.T) {
	m := NewConnectionManager()
	h := NewConnectionHandler(m)

	m.AddConnection(&ConnectionDetails{
		ID:       "conn-1",
		ClientIP: "192.168.1.1",
		Backend:  "default",
	})

	req := httptest.NewRequest("GET", "/api/connections", nil)
	rec := httptest.NewRecorder()

	h.handleList(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	var result ConnectionListResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if result.Total != 1 {
		t.Errorf("expected total 1, got %d", result.Total)
	}
}

func TestConnectionHandler_Detail(t *testing.T) {
	m := NewConnectionManager()
	h := NewConnectionHandler(m)

	m.AddConnection(&ConnectionDetails{
		ID:       "conn-1",
		ClientIP: "192.168.1.1",
	})

	// Found
	req := httptest.NewRequest("GET", "/api/connections/conn-1", nil)
	rec := httptest.NewRecorder()
	h.handleDetail(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	// Not found
	req = httptest.NewRequest("GET", "/api/connections/nonexistent", nil)
	rec = httptest.NewRecorder()
	h.handleDetail(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", rec.Code)
	}
}

func TestConnectionHandler_Stats(t *testing.T) {
	m := NewConnectionManager()
	h := NewConnectionHandler(m)

	m.AddConnection(&ConnectionDetails{ID: "1", Backend: "b1", State: "active"})
	m.AddConnection(&ConnectionDetails{ID: "2", Backend: "b2", State: "idle"})

	req := httptest.NewRequest("GET", "/api/connections/stats", nil)
	rec := httptest.NewRecorder()

	h.handleStats(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	var stats map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &stats); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if stats["total"].(float64) != 2 {
		t.Errorf("expected total 2, got %v", stats["total"])
	}
}

func TestConnectionDetails_Timestamps(t *testing.T) {
	m := NewConnectionManager()

	// Without explicit timestamps
	details := &ConnectionDetails{ID: "conn-1"}
	m.AddConnection(details)

	retrieved := m.GetConnection("conn-1")
	if retrieved.ConnectedAt.IsZero() {
		t.Error("expected ConnectedAt to be set")
	}
	if retrieved.LastActivity.IsZero() {
		t.Error("expected LastActivity to be set")
	}

	// With explicit timestamps
	m.AddConnection(&ConnectionDetails{
		ID:          "conn-2",
		ConnectedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
	})

	retrieved = m.GetConnection("conn-2")
	if !retrieved.ConnectedAt.Equal(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Error("expected ConnectedAt to be preserved")
	}
}