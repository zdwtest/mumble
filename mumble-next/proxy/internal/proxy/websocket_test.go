package proxy

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
)

func TestNewForwarder(t *testing.T) {
	wsConn := &websocket.Conn{}
	tcpConn := &net.TCPConn{}

	forwarder := NewForwarder(wsConn, tcpConn, 32*1024)

	if forwarder == nil {
		t.Fatal("expected forwarder to be created")
	}

	if forwarder.bufSize != 32*1024 {
		t.Errorf("expected bufSize 32768, got %d", forwarder.bufSize)
	}
}

func TestWebSocketHandler(t *testing.T) {
	// 创建一个测试 WebSocket 服务器
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()

		// 读取并回显消息
		for {
			mt, msg, err := ws.ReadMessage()
			if err != nil {
				break
			}
			ws.WriteMessage(mt, msg)
		}
	}))
	defer server.Close()

	// 连接到测试服务器
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect to test server: %v", err)
	}
	defer ws.Close()

	// 发送测试消息
	testMsg := []byte("hello")
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