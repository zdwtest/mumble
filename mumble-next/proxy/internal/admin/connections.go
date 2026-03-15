package admin

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"time"
)

// ConnectionDetails 连接详情
type ConnectionDetails struct {
	// ID 连接唯一标识
	ID string `json:"id"`

	// ClientIP 客户端 IP
	ClientIP string `json:"client_ip"`

	// Backend 后端名称
	Backend string `json:"backend"`

	// UserID 用户标识
	UserID string `json:"user_id,omitempty"`

	// ConnectedAt 连接时间
	ConnectedAt time.Time `json:"connected_at"`

	// LastActivity 最后活动时间
	LastActivity time.Time `json:"last_activity"`

	// BytesSent 发送字节数
	BytesSent int64 `json:"bytes_sent"`

	// BytesReceived 接收字节数
	BytesReceived int64 `json:"bytes_received"`

	// MessagesSent 发送消息数
	MessagesSent int64 `json:"messages_sent"`

	// MessagesReceived 接收消息数
	MessagesReceived int64 `json:"messages_received"`

	// State 连接状态
	State string `json:"state"` // connecting, active, closing, closed

	// Protocol 协议版本
	Protocol string `json:"protocol,omitempty"`

	// UserAgent 客户端 User-Agent
	UserAgent string `json:"user_agent,omitempty"`

	// Metadata 自定义元数据
	Metadata map[string]string `json:"metadata,omitempty"`
}

// ConnectionListParams 连接列表查询参数
type ConnectionListParams struct {
	// Backend 后端过滤
	Backend string `json:"backend,omitempty"`

	// State 状态过滤
	State string `json:"state,omitempty"`

	// Limit 返回数量限制
	Limit int `json:"limit,omitempty"`

	// Offset 偏移量
	Offset int `json:"offset,omitempty"`

	// SortBy 排序字段
	SortBy string `json:"sort_by,omitempty"` // id, connected_at, last_activity, bytes_sent

	// SortOrder 排序顺序
	SortOrder string `json:"sort_order,omitempty"` // asc, desc
}

// ConnectionListResult 连接列表结果
type ConnectionListResult struct {
	// Total 总数
	Total int `json:"total"`

	// Connections 连接列表
	Connections []*ConnectionDetails `json:"connections"`

	// Offset 当前偏移
	Offset int `json:"offset"`

	// Limit 当前限制
	Limit int `json:"limit"`
}

// ConnectionManager 连接管理器
type ConnectionManager struct {
	connections map[string]*ConnectionDetails
	mu          sync.RWMutex
}

// NewConnectionManager 创建连接管理器
func NewConnectionManager() *ConnectionManager {
	return &ConnectionManager{
		connections: make(map[string]*ConnectionDetails),
	}
}

// AddConnection 添加连接
func (m *ConnectionManager) AddConnection(details *ConnectionDetails) {
	if details.ID == "" {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if details.ConnectedAt.IsZero() {
		details.ConnectedAt = time.Now()
	}
	if details.LastActivity.IsZero() {
		details.LastActivity = time.Now()
	}
	if details.State == "" {
		details.State = "active"
	}

	m.connections[details.ID] = details
}

// RemoveConnection 移除连接
func (m *ConnectionManager) RemoveConnection(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.connections, id)
}

// GetConnection 获取单个连接详情
func (m *ConnectionManager) GetConnection(id string) *ConnectionDetails {
	m.mu.RLock()
	defer m.mu.RUnlock()

	conn, ok := m.connections[id]
	if !ok {
		return nil
	}

	// 返回副本
	copy := *conn
	return &copy
}

// UpdateConnection 更新连接信息
func (m *ConnectionManager) UpdateConnection(id string, updates func(*ConnectionDetails)) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if conn, ok := m.connections[id]; ok {
		updates(conn)
	}
}

// UpdateActivity 更新活动时间
func (m *ConnectionManager) UpdateActivity(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if conn, ok := m.connections[id]; ok {
		conn.LastActivity = time.Now()
	}
}

// AddBytes 增加流量统计
func (m *ConnectionManager) AddBytes(id string, sent, received int64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if conn, ok := m.connections[id]; ok {
		conn.BytesSent += sent
		conn.BytesReceived += received
		if sent > 0 {
			conn.MessagesSent++
		}
		if received > 0 {
			conn.MessagesReceived++
		}
		conn.LastActivity = time.Now()
	}
}

// SetState 设置连接状态
func (m *ConnectionManager) SetState(id, state string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if conn, ok := m.connections[id]; ok {
		conn.State = state
	}
}

// ListConnections 列出连接
func (m *ConnectionManager) ListConnections(params ConnectionListParams) *ConnectionListResult {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// 过滤
	var filtered []*ConnectionDetails
	for _, conn := range m.connections {
		if params.Backend != "" && conn.Backend != params.Backend {
			continue
		}
		if params.State != "" && conn.State != params.State {
			continue
		}
		// 创建副本
		copy := *conn
		filtered = append(filtered, &copy)
	}

	// 排序
	sort.Slice(filtered, func(i, j int) bool {
		switch params.SortBy {
		case "connected_at":
			if params.SortOrder == "desc" {
				return filtered[i].ConnectedAt.After(filtered[j].ConnectedAt)
			}
			return filtered[i].ConnectedAt.Before(filtered[j].ConnectedAt)
		case "last_activity":
			if params.SortOrder == "desc" {
				return filtered[i].LastActivity.After(filtered[j].LastActivity)
			}
			return filtered[i].LastActivity.Before(filtered[j].LastActivity)
		case "bytes_sent":
			if params.SortOrder == "desc" {
				return filtered[i].BytesSent > filtered[j].BytesSent
			}
			return filtered[i].BytesSent < filtered[j].BytesSent
		default: // id
			if params.SortOrder == "desc" {
				return filtered[i].ID > filtered[j].ID
			}
			return filtered[i].ID < filtered[j].ID
		}
	})

	// 分页
	total := len(filtered)
	limit := params.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	offset := params.Offset
	if offset < 0 {
		offset = 0
	}
	if offset > total {
		offset = total
	}

	end := offset + limit
	if end > total {
		end = total
	}

	return &ConnectionListResult{
		Total:       total,
		Connections: filtered[offset:end],
		Offset:      offset,
		Limit:       limit,
	}
}

// Count 获取连接数
func (m *ConnectionManager) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.connections)
}

// CountByBackend 按后端统计连接数
func (m *ConnectionManager) CountByBackend() map[string]int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	counts := make(map[string]int)
	for _, conn := range m.connections {
		counts[conn.Backend]++
	}
	return counts
}

// CountByState 按状态统计连接数
func (m *ConnectionManager) CountByState() map[string]int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	counts := make(map[string]int)
	for _, conn := range m.connections {
		counts[conn.State]++
	}
	return counts
}

// Clear 清空所有连接
func (m *ConnectionManager) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.connections = make(map[string]*ConnectionDetails)
}

// ConnectionHandler 连接详情 HTTP 处理器
type ConnectionHandler struct {
	manager *ConnectionManager
}

// NewConnectionHandler 创建连接处理器
func NewConnectionHandler(manager *ConnectionManager) *ConnectionHandler {
	return &ConnectionHandler{
		manager: manager,
	}
}

// RegisterRoutes 注册路由
func (h *ConnectionHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/connections", h.handleList)
	mux.HandleFunc("/api/connections/", h.handleDetail)
	mux.HandleFunc("/api/connections/stats", h.handleStats)
}

// handleList 处理连接列表请求
func (h *ConnectionHandler) handleList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	params := ConnectionListParams{
		Backend:    r.URL.Query().Get("backend"),
		State:      r.URL.Query().Get("state"),
		SortBy:     r.URL.Query().Get("sort"),
		SortOrder:  r.URL.Query().Get("order"),
	}

	if limit := r.URL.Query().Get("limit"); limit != "" {
		if l, err := strconv.Atoi(limit); err == nil {
			params.Limit = l
		}
	}

	if offset := r.URL.Query().Get("offset"); offset != "" {
		if o, err := strconv.Atoi(offset); err == nil {
			params.Offset = o
		}
	}

	result := h.manager.ListConnections(params)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// handleDetail 处理单个连接详情请求
func (h *ConnectionHandler) handleDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 提取 ID (去掉 /api/connections/ 前缀)
	id := r.URL.Path[len("/api/connections/"):]
	if id == "" {
		http.Error(w, "missing connection id", http.StatusBadRequest)
		return
	}

	conn := h.manager.GetConnection(id)
	if conn == nil {
		http.Error(w, "connection not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(conn)
}

// handleStats 处理连接统计请求
func (h *ConnectionHandler) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	stats := map[string]interface{}{
		"total":           h.manager.Count(),
		"by_backend":      h.manager.CountByBackend(),
		"by_state":        h.manager.CountByState(),
		"updated_at":      time.Now(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}