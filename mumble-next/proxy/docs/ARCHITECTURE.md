# 架构设计

## 概述

Mumble WebSocket Proxy 是一个高性能的 WebSocket 到 TCP 代理，采用模块化设计，支持多租户、安全认证和完善的可观测性。

## 系统架构

```
┌─────────────────────────────────────────────────────────────────┐
│                        Mumble WebSocket Proxy                    │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  ┌─────────────┐    ┌─────────────┐    ┌─────────────┐         │
│  │  WebSocket  │───▶│   Router    │───▶│   Backend   │         │
│  │   Handler   │    │  (Token)    │    │   Pool      │         │
│  └─────────────┘    └─────────────┘    └─────────────┘         │
│         │                                     │                  │
│         ▼                                     ▼                  │
│  ┌─────────────┐                     ┌─────────────┐           │
│  │ Middleware  │                     │   Murmur    │           │
│  │ - Auth      │                     │   Server    │           │
│  │ - RateLimit │                     └─────────────┘           │
│  │ - IPFilter  │                                                │
│  └─────────────┘                                                │
│         │                                                       │
│         ▼                                                       │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │                    Internal Components                   │   │
│  ├─────────────┬─────────────┬─────────────┬─────────────┤   │
│  │   Config    │   Metrics   │   Health    │   Audit     │   │
│  │   Manager   │   Collector │   Checker   │   Logger    │   │
│  └─────────────┴─────────────┴─────────────┴─────────────┘   │
│                                                                │
└─────────────────────────────────────────────────────────────────┘
```

## 模块设计

### 1. 代理核心 (`internal/proxy`)

#### 1.1 WebSocket 处理器

```go
type WebSocketHandler struct {
    upgrader    websocket.Upgrader
    backendPool *BackendPool
    metrics     *metrics.Metrics
    logger      *zap.Logger
}

// 处理流程
// 1. HTTP 升级 WebSocket
// 2. Token 认证确定后端
// 3. 创建 TCP 连接
// 4. 双向数据转发
// 5. 心跳保活
// 6. 优雅关闭
```

#### 1.2 连接管理

```go
type ClientConn struct {
    ID           string
    WSConn       *websocket.Conn
    TCPConn      net.Conn
    Backend      string
    ConnectedAt  time.Time
    BytesSent    int64
    BytesReceived int64
    done         chan struct{}
}
```

#### 1.3 心跳机制

```go
type HeartbeatManager struct {
    interval    time.Duration
    timeout     time.Duration
    pingChan    chan struct{}
    pongChan    chan struct{}
    missedPongs int
    maxMissed   int
}
```

### 2. 认证系统 (`internal/auth`)

#### 2.1 Token 认证

```go
type TokenManager struct {
    backendTokens map[string][]string // backend -> tokens
    mu            sync.RWMutex
}

func (tm *TokenManager) Validate(token string) (backend string, ok bool)
```

#### 2.2 TLS 客户端证书

```go
type TLSCertManager struct {
    cert       *tls.Certificate
    caCertPool *x509.CertPool
    allowedCNs []string
    allowedOUs []string
    crls       []*x509.RevocationList
}

func (m *TLSCertManager) VerifyClientCert(rawCerts [][]byte) error
func (m *TLSCertManager) GetClientIdentity(cert *x509.Certificate) (cn, ou string)
```

#### 2.3 请求签名

```go
type SignatureVerifier struct {
    secrets  map[string]string // keyID -> secret
    nonces   *nonceCache
    maxSkew  time.Duration
}

// 签名格式: HMAC-SHA256(keyID:timestamp:nonce:method:path:bodyHash, secret)
func (sv *SignatureVerifier) Verify(r *http.Request) (bool, error)
```

### 3. 连接池 (`internal/pool`)

```go
type ConnPool struct {
    host        string
    port        int
    maxIdle     int
    maxTotal    int
    idleTimeout time.Duration

    idleConns   chan net.Conn
    activeCount int
    mu          sync.Mutex
}

func (p *ConnPool) Get(ctx context.Context) (net.Conn, error)
func (p *ConnPool) Put(conn net.Conn)
```

### 4. 可靠性机制 (`internal/proxy`)

#### 4.1 重连机制

```go
type Reconnector struct {
    config      ReconnectConfig
    attempt     int
    state       ReconnectState

    // 指数退避
    // delay = initialDelay * multiplier^attempt
    // max delay = 30s
}
```

#### 4.2 熔断器

```go
type CircuitBreaker struct {
    state          CircuitState  // Closed, Open, HalfOpen
    failureCount   int
    threshold      int
    timeout        time.Duration
    lastFailure    time.Time
}

// 状态转换
// Closed -> Open (失败数 >= 阈值)
// Open -> HalfOpen (超时后)
// HalfOpen -> Closed (成功) 或 Open (失败)
```

### 5. 可观测性

#### 5.1 Prometheus 指标

```go
type Metrics struct {
    // 连接指标
    ActiveConnections  promauto.Gauge
    TotalConnections   promauto.Counter
    ConnectionDuration promauto.Histogram

    // 流量指标
    BytesTransferred *promauto.CounterVec

    // 错误指标
    Errors *promauto.CounterVec
}
```

#### 5.2 健康检查

```go
type HealthChecker struct {
    murmurHost string
    murmurPort int
}

type HealthStatus struct {
    Status    string           `json:"status"`
    Timestamp time.Time        `json:"timestamp"`
    Checks    map[string]Check `json:"checks"`
}
```

#### 5.3 审计日志

```go
type Event struct {
    Timestamp time.Time              `json:"timestamp"`
    Type      EventType              `json:"type"`
    ClientIP  string                 `json:"client_ip,omitempty"`
    Backend   string                 `json:"backend,omitempty"`
    Action    string                 `json:"action"`
    Result    string                 `json:"result"`
    Details   map[string]interface{} `json:"details,omitempty"`
}

// 事件类型
const (
    EventConnect      EventType = "connect"
    EventDisconnect   EventType = "disconnect"
    EventAuthSuccess  EventType = "auth_success"
    EventAuthFailure  EventType = "auth_failure"
    EventIPBlocked    EventType = "ip_blocked"
    EventRateLimited  EventType = "rate_limited"
)
```

## 数据流

### 连接建立流程

```
Client                Proxy                 Murmur
  │                    │                      │
  │─── WebSocket ─────▶│                      │
  │     Upgrade        │                      │
  │                    │                      │
  │◀─── 101 Switch ────│                      │
  │                    │                      │
  │─── Token ─────────▶│                      │
  │                    │                      │
  │                    │─── TCP Connect ─────▶│
  │                    │                      │
  │                    │◀─── Accept ──────────│
  │                    │                      │
  │◀─── Connected ─────│                      │
  │                    │                      │
  │◀─── heartbeat ────▶│◀──── heartbeat ─────▶│
```

### 数据转发

```
Client                Proxy                 Murmur
  │                    │                      │
  │─── WebSocket ─────▶│                      │
  │     Message        │                      │
  │                    │─── TCP Data ────────▶│
  │                    │                      │
  │                    │◀─── TCP Data ────────│
  │◀─── WebSocket ─────│                      │
  │     Message        │                      │
```

## 配置热重载

```go
type ConfigWatcher struct {
    config      atomic.Pointer[Config]
    callbacks   []func(old, new *Config)
    watcher     *fsnotify.Watcher
    debounce    time.Duration
}

// 可重载配置
// - Backends
// - Auth tokens
// - Rate limits
// - Logging level
// - IP filters

// 不可重载配置 (需重启)
// - Server host/port
// - TLS certificates
// - Metrics port
```

## 性能优化

### 1. Buffer 池化

```go
type BufferPool struct {
    pools map[int]*sync.Pool  // size -> pool
}

// 预定义大小: 1KB, 4KB, 16KB, 64KB
```

### 2. 连接复用

```go
// 连接池复用 TCP 连接
// 空闲超时: 5 分钟
// 最大生命周期: 30 分钟
```

### 3. WebSocket 压缩

```go
type Compressor struct {
    config    CompressionConfig
    flatePool sync.Pool
}

// permessage-deflate 压缩
// 阈值: 256 bytes
// 级别: default (level 6)
```

## 安全设计

### 深度防御

```
┌─────────────────────────────────────────┐
│            外部网络                      │
└────────────────────┬────────────────────┘
                     │
        ┌────────────▼────────────┐
        │      IP 过滤层          │
        │  - 白名单               │
        │  - 黑名单               │
        └────────────┬────────────┘
                     │
        ┌────────────▼────────────┐
        │      TLS 层             │
        │  - 证书验证             │
        │  - 客户端证书           │
        └────────────┬────────────┘
                     │
        ┌────────────▼────────────┐
        │      认证层             │
        │  - Token 认证           │
        │  - 请求签名             │
        └────────────┬────────────┘
                     │
        ┌────────────▼────────────┐
        │      限流层             │
        │  - 连接数限制           │
        │  - 请求速率限制         │
        └────────────┬────────────┘
                     │
        ┌────────────▼────────────┐
        │      代理核心           │
        └─────────────────────────┘
```

## 测试策略

### 单元测试

- 所有模块覆盖率 ≥ 80%
- 使用 mock 隔离外部依赖
- 并发安全测试

### 集成测试

- Mock Murmur 服务器
- 端到端连接测试
- 故障注入测试

### 基准测试

```go
func BenchmarkProxy_Forward(b *testing.B) {
    // 测试数据转发性能
}

func BenchmarkProxy_Concurrent(b *testing.B) {
    // 测试并发连接性能
}
```

## 扩展点

### 自定义认证

```go
type AuthProvider interface {
    Authenticate(r *http.Request) (backend string, ok bool)
}
```

### 自定义指标

```go
type MetricsCollector interface {
    Collect(ch chan<- prometheus.Metric)
    Describe(ch chan<- *prometheus.Desc)
}
```

### 自定义中间件

```go
type Middleware func(http.Handler) http.Handler

// 使用
chain := middleware.Chain(
    middleware.Logging,
    middleware.Recovery,
    middleware.Auth,
)
```