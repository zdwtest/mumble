package e2e

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// MockMurmur 模拟 Murmur 服务器
type MockMurmur struct {
	listener net.Listener
	clients  []net.Conn
	done     chan struct{}
}

// NewMockMurmur 创建模拟 Murmur 服务器
func NewMockMurmur() *MockMurmur {
	return &MockMurmur{
		clients: make([]net.Conn, 0),
		done:    make(chan struct{}),
	}
}

// Start 启动模拟服务器
func (m *MockMurmur) Start(port int) error {
	addr := net.JoinHostPort("127.0.0.1", fmt.Sprintf("%d", port))
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	m.listener = listener

	go m.acceptLoop()
	return nil
}

func (m *MockMurmur) acceptLoop() {
	for {
		conn, err := m.listener.Accept()
		if err != nil {
			select {
			case <-m.done:
				return
			default:
				continue
			}
		}
		m.clients = append(m.clients, conn)
		go m.handleConn(conn)
	}
}

func (m *MockMurmur) handleConn(conn net.Conn) {
	buf := make([]byte, 1024)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			return
		}
		// 回显数据
		conn.Write(buf[:n])
	}
}

// Stop 停止模拟服务器
func (m *MockMurmur) Stop() {
	close(m.done)
	for _, c := range m.clients {
		c.Close()
	}
	if m.listener != nil {
		m.listener.Close()
	}
}

// TestE2E_FullConnectionFlow 测试完整连接流程
func TestE2E_FullConnectionFlow(t *testing.T) {
	// 启动模拟 Murmur 服务器
	mockMurmur := NewMockMurmur()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start mock murmur: %v", err)
	}
	mockMurmur.listener = listener
	go mockMurmur.acceptLoop()
	defer mockMurmur.Stop()

	// 创建 WebSocket 测试服务器
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()

		// 连接到模拟 Murmur
		murmurAddr := mockMurmur.listener.Addr().String()
		tcp, err := net.Dial("tcp", murmurAddr)
		if err != nil {
			ws.WriteMessage(websocket.CloseMessage,
				websocket.FormatCloseMessage(1011, "unable to connect"))
			return
		}
		defer tcp.Close()

		// 双向转发
		done := make(chan struct{}, 2)

		go func() {
			defer func() { done <- struct{}{} }()
			for {
				_, data, err := ws.ReadMessage()
				if err != nil {
					return
				}
				tcp.Write(data)
			}
		}()

		go func() {
			defer func() { done <- struct{}{} }()
			buf := make([]byte, 1024)
			for {
				n, err := tcp.Read(buf)
				if err != nil {
					return
				}
				ws.WriteMessage(websocket.BinaryMessage, buf[:n])
			}
		}()

		<-done
	}))
	defer server.Close()

	// 连接到测试服务器
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer ws.Close()

	// 发送测试消息
	testMsg := []byte("test message")
	if err := ws.WriteMessage(websocket.BinaryMessage, testMsg); err != nil {
		t.Fatalf("failed to send message: %v", err)
	}

	// 接收回显
	_, msg, err := ws.ReadMessage()
	if err != nil {
		t.Fatalf("failed to receive message: %v", err)
	}

	if string(msg) != string(testMsg) {
		t.Errorf("expected %s, got %s", testMsg, msg)
	}
}

// TestE2E_MultipleClients 测试多客户端连接
func TestE2E_MultipleClients(t *testing.T) {
	// 启动模拟 Murmur 服务器
	mockMurmur := NewMockMurmur()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start mock murmur: %v", err)
	}
	mockMurmur.listener = listener
	go mockMurmur.acceptLoop()
	defer mockMurmur.Stop()

	// 创建 WebSocket 测试服务器
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()

		murmurAddr := mockMurmur.listener.Addr().String()
		tcp, err := net.Dial("tcp", murmurAddr)
		if err != nil {
			return
		}
		defer tcp.Close()

		done := make(chan struct{}, 2)

		go func() {
			defer func() { done <- struct{}{} }()
			for {
				_, data, err := ws.ReadMessage()
				if err != nil {
					return
				}
				tcp.Write(data)
			}
		}()

		go func() {
			defer func() { done <- struct{}{} }()
			buf := make([]byte, 1024)
			for {
				n, err := tcp.Read(buf)
				if err != nil {
					return
				}
				ws.WriteMessage(websocket.BinaryMessage, buf[:n])
			}
		}()

		<-done
	}))
	defer server.Close()

	// 创建多个客户端
	numClients := 5
	clients := make([]*websocket.Conn, numClients)

	for i := 0; i < numClients; i++ {
		wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
		ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			t.Fatalf("client %d failed to connect: %v", i, err)
		}
		clients[i] = ws
	}

	// 清理
	for _, ws := range clients {
		ws.Close()
	}
}

// TestE2E_LargeMessage 测试大消息传输
func TestE2E_LargeMessage(t *testing.T) {
	// 启动模拟 Murmur 服务器
	mockMurmur := NewMockMurmur()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start mock murmur: %v", err)
	}
	mockMurmur.listener = listener
	go mockMurmur.acceptLoop()
	defer mockMurmur.Stop()

	// 创建 WebSocket 测试服务器
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()

		murmurAddr := mockMurmur.listener.Addr().String()
		tcp, err := net.Dial("tcp", murmurAddr)
		if err != nil {
			return
		}
		defer tcp.Close()

		done := make(chan struct{}, 2)

		go func() {
			defer func() { done <- struct{}{} }()
			for {
				_, data, err := ws.ReadMessage()
				if err != nil {
					return
				}
				tcp.Write(data)
			}
		}()

		go func() {
			defer func() { done <- struct{}{} }()
			buf := make([]byte, 64*1024)
			for {
				n, err := tcp.Read(buf)
				if err != nil {
					return
				}
				ws.WriteMessage(websocket.BinaryMessage, buf[:n])
			}
		}()

		<-done
	}))
	defer server.Close()

	// 连接
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer ws.Close()

	// 发送较大消息 (4KB) - 使用较小的消息以确保测试稳定性
	largeMsg := make([]byte, 4*1024)
	for i := range largeMsg {
		largeMsg[i] = byte(i % 256)
	}

	if err := ws.WriteMessage(websocket.BinaryMessage, largeMsg); err != nil {
		t.Fatalf("failed to send large message: %v", err)
	}

	// 接收回显 - 可能分多次接收
	received := make([]byte, 0)
	for len(received) < len(largeMsg) {
		_, msg, err := ws.ReadMessage()
		if err != nil {
			t.Fatalf("failed to receive message: %v", err)
		}
		received = append(received, msg...)
	}

	// 验证接收的数据
	for i := 0; i < len(largeMsg); i++ {
		if received[i] != largeMsg[i] {
			t.Errorf("data mismatch at position %d: expected %d, got %d", i, largeMsg[i], received[i])
			break
		}
	}
}

// TestE2E_ConnectionTimeout 测试连接超时
func TestE2E_ConnectionTimeout(t *testing.T) {
	// 创建 WebSocket 测试服务器 (不启动 Murmur)
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()

		// 尝试连接到不可达的 Murmur
		_, err = net.DialTimeout("tcp", "127.0.0.1:1", 1*time.Second)
		if err != nil {
			ws.WriteMessage(websocket.CloseMessage,
				websocket.FormatCloseMessage(1011, "unable to connect"))
			return
		}
	}))
	defer server.Close()

	// 连接
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer ws.Close()

	// 应该收到关闭消息
	_, _, err = ws.ReadMessage()
	if err == nil {
		t.Error("expected connection to be closed")
	}
}