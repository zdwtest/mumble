package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestConnectionHandler_MethodNotAllowed(t *testing.T) {
	m := NewConnectionManager()
	h := NewConnectionHandler(m)

	// POST to list endpoint
	req := httptest.NewRequest("POST", "/api/connections", nil)
	rec := httptest.NewRecorder()
	h.handleList(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", rec.Code)
	}

	// POST to detail endpoint
	req = httptest.NewRequest("POST", "/api/connections/conn-1", nil)
	rec = httptest.NewRecorder()
	h.handleDetail(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", rec.Code)
	}

	// POST to stats endpoint
	req = httptest.NewRequest("POST", "/api/connections/stats", nil)
	rec = httptest.NewRecorder()
	h.handleStats(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", rec.Code)
	}
}

func TestConnectionHandler_EmptyID(t *testing.T) {
	m := NewConnectionManager()
	h := NewConnectionHandler(m)

	req := httptest.NewRequest("GET", "/api/connections/", nil)
	rec := httptest.NewRecorder()
	h.handleDetail(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rec.Code)
	}
}

func TestConnectionManager_UpdateNonExistent(t *testing.T) {
	m := NewConnectionManager()

	// Update non-existent connection should not panic
	m.UpdateConnection("non-existent", func(d *ConnectionDetails) {
		d.State = "updated"
	})

	m.AddBytes("non-existent", 100, 200)
	m.SetState("non-existent", "closed")
	m.UpdateActivity("non-existent")
}

func TestConnectionManager_AddEmptyID(t *testing.T) {
	m := NewConnectionManager()

	// Empty ID should be ignored
	m.AddConnection(&ConnectionDetails{ID: ""})

	if m.Count() != 0 {
		t.Error("expected 0 connections for empty ID")
	}
}

func TestConnectionManager_ListSorting(t *testing.T) {
	m := NewConnectionManager()

	// Add connections with different times
	now, _ := time.Parse(time.RFC3339, "2024-01-01T12:00:00Z")
	m.AddConnection(&ConnectionDetails{
		ID:          "a",
		ConnectedAt: now,
		BytesSent:   100,
	})
	m.AddConnection(&ConnectionDetails{
		ID:          "b",
		ConnectedAt: now.Add(time.Hour),
		BytesSent:   200,
	})
	m.AddConnection(&ConnectionDetails{
		ID:          "c",
		ConnectedAt: now.Add(-time.Hour),
		BytesSent:   300,
	})

	// Sort by connected_at desc
	result := m.ListConnections(ConnectionListParams{
		SortBy:    "connected_at",
		SortOrder: "desc",
	})

	if result.Connections[0].ID != "b" {
		t.Errorf("expected first connection 'b', got %s", result.Connections[0].ID)
	}

	// Sort by bytes_sent asc
	result = m.ListConnections(ConnectionListParams{
		SortBy:    "bytes_sent",
		SortOrder: "asc",
	})

	if result.Connections[0].ID != "a" {
		t.Errorf("expected first connection 'a', got %s", result.Connections[0].ID)
	}
}

func TestConnectionManager_ListPaginationEdge(t *testing.T) {
	m := NewConnectionManager()

	for i := 0; i < 5; i++ {
		m.AddConnection(&ConnectionDetails{ID: string(rune('a' + i))})
	}

	// Offset beyond total
	result := m.ListConnections(ConnectionListParams{Offset: 100, Limit: 10})
	if len(result.Connections) != 0 {
		t.Error("expected 0 connections for offset beyond total")
	}

	// Negative offset
	result = m.ListConnections(ConnectionListParams{Offset: -1, Limit: 10})
	if result.Offset != 0 {
		t.Error("expected offset to be normalized to 0")
	}

	// Limit > 500
	result = m.ListConnections(ConnectionListParams{Limit: 1000})
	if result.Limit != 500 {
		t.Errorf("expected limit to be capped at 500, got %d", result.Limit)
	}

	// Limit <= 0
	result = m.ListConnections(ConnectionListParams{Limit: 0})
	if result.Limit != 50 {
		t.Errorf("expected default limit 50, got %d", result.Limit)
	}
}

func TestConnectionManager_AddBytesZero(t *testing.T) {
	m := NewConnectionManager()
	m.AddConnection(&ConnectionDetails{ID: "conn-1"})

	// Zero bytes should not increment message count
	m.AddBytes("conn-1", 0, 0)

	conn := m.GetConnection("conn-1")
	if conn.MessagesSent != 0 || conn.MessagesReceived != 0 {
		t.Error("expected 0 messages for zero bytes")
	}

	// Non-zero should increment
	m.AddBytes("conn-1", 100, 0)
	conn = m.GetConnection("conn-1")
	if conn.MessagesSent != 1 {
		t.Errorf("expected 1 sent message, got %d", conn.MessagesSent)
	}
	if conn.MessagesReceived != 0 {
		t.Errorf("expected 0 received messages, got %d", conn.MessagesReceived)
	}
}

func TestConnectionHandler_ListWithFilters(t *testing.T) {
	m := NewConnectionManager()
	h := NewConnectionHandler(m)

	m.AddConnection(&ConnectionDetails{ID: "1", Backend: "b1", State: "active"})
	m.AddConnection(&ConnectionDetails{ID: "2", Backend: "b1", State: "idle"})
	m.AddConnection(&ConnectionDetails{ID: "3", Backend: "b2", State: "active"})

	// Filter by backend
	req := httptest.NewRequest("GET", "/api/connections?backend=b1", nil)
	rec := httptest.NewRecorder()
	h.handleList(rec, req)

	var result ConnectionListResult
	_ = json.Unmarshal(rec.Body.Bytes(), &result)
	if result.Total != 2 {
		t.Errorf("expected 2 connections for backend b1, got %d", result.Total)
	}

	// Filter by state
	req = httptest.NewRequest("GET", "/api/connections?state=active", nil)
	rec = httptest.NewRecorder()
	h.handleList(rec, req)

	_ = json.Unmarshal(rec.Body.Bytes(), &result)
	if result.Total != 2 {
		t.Errorf("expected 2 active connections, got %d", result.Total)
	}

	// With limit and offset
	req = httptest.NewRequest("GET", "/api/connections?limit=1&offset=1", nil)
	rec = httptest.NewRecorder()
	h.handleList(rec, req)

	_ = json.Unmarshal(rec.Body.Bytes(), &result)
	if len(result.Connections) != 1 {
		t.Errorf("expected 1 connection, got %d", len(result.Connections))
	}
}

func TestConnectionHandler_RegisterRoutes(t *testing.T) {
	m := NewConnectionManager()
	h := NewConnectionHandler(m)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	// Verify routes are registered
	routes := []string{
		"/api/connections",
		"/api/connections/stats",
	}

	for _, route := range routes {
		req := httptest.NewRequest("GET", route, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("route %s: expected status 200, got %d", route, rec.Code)
		}
	}
}

func TestConnectionDetails_Metadata(t *testing.T) {
	m := NewConnectionManager()

	details := &ConnectionDetails{
		ID:     "conn-1",
		UserID: "user-1",
		Metadata: map[string]string{
			"key1": "value1",
			"key2": "value2",
		},
	}

	m.AddConnection(details)

	conn := m.GetConnection("conn-1")
	if conn.Metadata["key1"] != "value1" {
		t.Error("expected metadata to be preserved")
	}
}