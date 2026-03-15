package proxy

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	"github.com/mumble/mumble-next/proxy/internal/config"
	"github.com/mumble/mumble-next/proxy/internal/metrics"
)

// BenchmarkWebSocketForward benchmarks WebSocket to TCP forwarding
func BenchmarkWebSocketForward(b *testing.B) {
	// Start TCP server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		b.Fatalf("failed to start server: %v", err)
	}
	defer listener.Close()

	addr := listener.Addr().(*net.TCPAddr)

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go echoHandler(conn)
		}
	}()

	cfg := config.DefaultConfig()
	cfg.Murmur.Host = "127.0.0.1"
	cfg.Murmur.Port = addr.Port
	cfg.Murmur.Timeout = 5 * time.Second

	logger := zap.NewNop()
	m := metrics.NewNoopMetrics()
	p := NewProxyWithMetrics(cfg, logger, m)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.HandleWebSocket(w, r)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/mumble"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			b.Fatalf("failed to connect: %v", err)
		}

		// Send and receive
		ws.WriteMessage(websocket.BinaryMessage, []byte("hello"))
		ws.ReadMessage()
		ws.Close()
	}
}

// BenchmarkBufferPool benchmarks buffer pool operations
func BenchmarkBufferPool(b *testing.B) {
	pool := NewBufferPool()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf := pool.Get(4096)
		pool.Put(buf)
	}
}

// BenchmarkGlobalBufferPool benchmarks global buffer pool
func BenchmarkGlobalBufferPool(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf := GetBuffer(4096)
		PutBuffer(buf)
	}
}

// BenchmarkSizedPool benchmarks sized pool operations
func BenchmarkSizedPool(b *testing.B) {
	pool := NewSizedPool(4096)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf := pool.Get()
		pool.Put(buf)
	}
}

// BenchmarkRateLimiter benchmarks rate limiter operations
func BenchmarkRateLimiter(b *testing.B) {
	limiter := NewRateLimiter(1000)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		limiter.Allow("192.168.1.1")
		limiter.Release("192.168.1.1")
	}
}

// BenchmarkGenerateClientID benchmarks client ID generation
func BenchmarkGenerateClientID(b *testing.B) {
	for i := 0; i < b.N; i++ {
		generateClientID()
	}
}

// BenchmarkMaskToken benchmarks token masking
func BenchmarkMaskToken(b *testing.B) {
	token := "abcdefghijklmnopqrstuvwxyz123456"
	for i := 0; i < b.N; i++ {
		maskToken(token)
	}
}

// BenchmarkForwarder benchmarks data forwarding
func BenchmarkForwarder(b *testing.B) {
	// Start TCP server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		b.Fatalf("failed to start server: %v", err)
	}
	defer listener.Close()

	addr := listener.Addr().(*net.TCPAddr)

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go echoHandler(conn)
		}
	}()

	// Create WebSocket server
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()

		tcp, err := net.Dial("tcp", addr.String())
		if err != nil {
			return
		}
		defer tcp.Close()

		forwarder := NewForwarder(ws, tcp, 32*1024)
		done := make(chan struct{})
		go forwarder.ForwardWebSocketToTCP(func(err error) { close(done) })
		<-done
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	data := make([]byte, 1024)
	for i := range data {
		data[i] = byte(i % 256)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ws, _, _ := websocket.DefaultDialer.Dial(wsURL, nil)
		if ws != nil {
			ws.WriteMessage(websocket.BinaryMessage, data)
			ws.Close()
		}
	}
}

// BenchmarkCircuitBreaker benchmarks circuit breaker operations
func BenchmarkCircuitBreaker(b *testing.B) {
	cb := NewCircuitBreaker(5, time.Second)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cb.Allow()
		if i%2 == 0 {
			cb.RecordFailure()
		} else {
			cb.RecordSuccess()
		}
	}
}

// BenchmarkExponentialBackoff benchmarks backoff calculation
func BenchmarkExponentialBackoff(b *testing.B) {
	backoff := NewExponentialBackoff(100*time.Millisecond, 5*time.Second, 2.0)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		backoff.Next()
		backoff.Reset()
	}
}

// BenchmarkConcurrentConnections benchmarks handling multiple concurrent connections
func BenchmarkConcurrentConnections(b *testing.B) {
	// Start TCP server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		b.Fatalf("failed to start server: %v", err)
	}
	defer listener.Close()

	addr := listener.Addr().(*net.TCPAddr)

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go echoHandler(conn)
		}
	}()

	cfg := config.DefaultConfig()
	cfg.Murmur.Host = "127.0.0.1"
	cfg.Murmur.Port = addr.Port

	logger := zap.NewNop()
	m := metrics.NewNoopMetrics()
	p := NewProxyWithMetrics(cfg, logger, m)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.HandleWebSocket(w, r)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/mumble"

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			ws, _, _ := websocket.DefaultDialer.Dial(wsURL, nil)
			if ws != nil {
				ws.WriteMessage(websocket.BinaryMessage, []byte("test"))
				ws.ReadMessage()
				ws.Close()
			}
		}
	})
}

// BenchmarkStatsRead benchmarks reading connection stats
func BenchmarkStatsRead(b *testing.B) {
	cfg := config.DefaultConfig()
	logger := zap.NewNop()
	m := metrics.NewNoopMetrics()
	p := NewProxyWithMetrics(cfg, logger, m)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = p.Stats()
	}
}

// ExampleBufferPool demonstrates buffer pool usage
func ExampleBufferPool() {
	pool := NewBufferPool()

	// Get a buffer
	buf := pool.Get(1024)
	fmt.Printf("Got buffer of size: %d\n", len(buf))

	// Use the buffer...
	// buf = append(buf, data...)

	// Return the buffer
	pool.Put(buf)

	// Output: Got buffer of size: 1024
}