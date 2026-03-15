# Mumble WebSocket Proxy (Commercial Edition)

[![CI](https://github.com/mumble/mumble/workflows/CI/badge.svg)](https://github.com/mumble/mumble/actions)
[![Go Report Card](https://goreportcard.com/badge/github.com/mumble/mumble-next/proxy)](https://goreportcard.com/report/github.com/mumble/mumble-next/proxy)
[![License](https://img.shields.io/badge/License-BSD%203--Clause-blue.svg)](LICENSE)
[![Coverage](https://img.shields.io/badge/Coverage-88.6%25-brightgreen)]()

WebSocket 到 TCP 的透明代理，让 Web 客户端可以连接 Murmur 服务器。

## 特性

### 核心功能
- **透明代理**: WebSocket ↔ TCP 双向数据转发
- **多租户支持**: 基于 Token 的后端路由
- **UDP 代理**: 可选的 UDP 音频代理
- **WebSocket 压缩**: permessage-deflate 支持

### 安全性
- **Token 认证**: 支持多后端路由
- **TLS 客户端证书**: mTLS 双向认证
- **请求签名验证**: HMAC-SHA256 签名
- **IP 过滤**: 白名单/黑名单支持
- **审计日志**: 完整操作记录

### 可靠性
- **自动重连**: 指数退避重连机制
- **熔断器保护**: 故障自动熔断
- **心跳检测**: Ping-Pong 保活
- **连接池**: 高效连接复用

### 可观测性
- **Prometheus 指标**: 连接数、字节数、延迟等
- **健康检查**: `/health`, `/healthz`, `/readyz`
- **实时日志流**: WebSocket 日志订阅
- **性能分析**: pprof 端点
- **优雅关闭**: 连接排空 (Drain) 支持

### 管理功能
- **配置热重载**: 无需重启更新配置
- **连接详情 API**: 实时连接状态查看
- **动态后端管理**: 运行时添加/删除后端

## 快速开始

### 使用 Docker

```bash
# 构建镜像
docker build -t mumble-proxy:latest .

# 运行
docker run -d \
  -p 8080:8080 \
  -p 9090:9090 \
  -e MUMBLE_MURMUR_HOST=mumble.example.com \
  -e MUMBLE_MURMUR_PORT=64738 \
  mumble-proxy:latest
```

### Docker Compose

```bash
cd deployments/docker
docker-compose up -d
```

### 从源码构建

```bash
# 克隆仓库
git clone https://github.com/mumble/mumble.git
cd mumble/mumble-next/proxy

# 构建
make build

# 运行
./bin/mumble-proxy --config configs/config.yaml
```

### 访问

| 端点 | 描述 |
|------|------|
| http://localhost:8080/ | Web 客户端 |
| ws://localhost:8080/mumble | WebSocket 端点 |
| http://localhost:8080/health | 健康检查 |
| http://localhost:8080/api/connections | 连接管理 |
| http://localhost:9090/metrics | Prometheus 指标 |

## 配置

### 完整配置示例

```yaml
# configs/config.yaml
server:
  host: "0.0.0.0"
  port: 8080
  read_timeout: 30s
  write_timeout: 30s
  tls:
    enabled: false
    cert_file: ""
    key_file: ""

murmur:
  host: "mumble.example.com"
  port: 64738
  timeout: 10s
  reconnect:
    enabled: true
    max_attempts: 3
    delay: 5s

# 多后端支持
auth:
  enabled: true
  header: "X-Mumble-Token"
  query_param: "token"

backends:
  - name: "production"
    host: "mumble-prod.example.com"
    port: 64738
    tokens: ["prod-token-1", "prod-token-2"]
  - name: "staging"
    host: "mumble-staging.example.com"
    port: 64738
    tokens: ["staging-token-1"]

# 安全配置
security:
  tls_client_cert:
    enabled: false
    ca_file: "/path/to/ca.pem"
    client_auth: require-and-verify
    allowed_cns: ["client1", "client2"]

  signature:
    enabled: false
    header: "X-Signature"
    max_skew: 5m
    secrets:
      - key_id: "key1"
        secret: "your-secret-key"

  ip_filter:
    whitelist_enabled: false
    whitelist: ["192.168.1.0/24", "10.0.0.1"]
    blacklist_enabled: false
    blacklist: ["192.168.100.0/24"]

# 性能配置
performance:
  pool:
    max_idle: 100
    max_total: 1000
    idle_timeout: 5m

  compression:
    enabled: true
    level: default
    threshold: 256

  circuit_breaker:
    enabled: true
    threshold: 5
    timeout: 30s

# 监控配置
metrics:
  enabled: true
  port: 9090
  path: /metrics

logging:
  level: info
  format: json

# 审计日志
audit:
  enabled: true
  file_path: logs/audit.log
  format: json
  buffer_size: 100
  flush_interval: 5s

limits:
  max_connections: 1000
  max_connections_per_ip: 10
  connection_timeout: 300s
```

### 环境变量

```bash
export MUMBLE_SERVER_PORT=8080
export MUMBLE_MURMUR_HOST=mumble.example.com
export MUMBLE_MURMUR_PORT=64738
export MUMBLE_METRICS_ENABLED=true
export MUMBLE_LOGGING_LEVEL=debug
```

## API 端点

### WebSocket 代理

```
ws://localhost:8080/mumble              # 默认后端
ws://localhost:8080/mumble?token=xxx    # Token 指定后端
```

### 健康检查

```
GET /health              # 健康状态
GET /health/ready        # 就绪状态
GET /health/live         # 存活状态
```

### 连接管理

```
GET /api/connections                    # 连接列表
GET /api/connections/{id}               # 连接详情
GET /api/connections/stats              # 连接统计
```

### 后端管理

```
GET /api/backends                       # 后端列表
POST /api/backends                      # 创建后端
DELETE /api/backends/{name}             # 删除后端
```

### 日志管理

```
GET /api/logs                           # 获取日志
WS /api/logs/stream                     # 实时日志流
```

### 配置管理

```
GET /api/config                         # 获取配置
POST /api/reload                        # 重载配置
```

### 性能分析 (需认证)

```
GET /debug/pprof/                       # 索引页
GET /debug/pprof/profile?seconds=30     # CPU 分析
GET /debug/pprof/heap                   # 堆内存分析
GET /debug/pprof/goroutine              # Goroutine 分析
GET /debug/pprof/trace?seconds=5        # 执行追踪
```

## 监控

### Prometheus 指标

```
mumble_proxy_connections_active         # 当前活跃连接数
mumble_proxy_connections_total          # 总连接数
mumble_proxy_bytes_sent_total           # 发送字节总数
mumble_proxy_bytes_received_total       # 接收字节总数
mumble_proxy_connection_duration_seconds # 连接持续时间
mumble_proxy_backend_latency_seconds    # 后端延迟
mumble_proxy_errors_total               # 错误总数
```

### Grafana Dashboard

导入 `deployments/grafana/` 中的仪表板配置。

## 测试覆盖率

| 模块 | 覆盖率 |
|------|--------|
| health | 97.7% |
| auth | 90.8% |
| drain | 95.6% |
| metrics | 95.0% |
| middleware | 94.6% |
| audit | 88.0% |
| config | 87.9% |
| pool | 84.6% |
| proxy | 83.8% |
| admin | 83.7% |

**总覆盖率: 88.6%**

## 部署

### Docker Compose

```bash
cd deployments/docker
docker-compose up -d
```

### Kubernetes

```bash
kubectl apply -f deployments/kubernetes/
```

## 开发

### 运行测试

```bash
# 所有测试
make test

# 测试覆盖率
make test-coverage

# 基准测试
make bench
```

### 代码检查

```bash
make lint
```

## 架构

```
┌─────────────┐                     ┌─────────────┐                     ┌─────────────┐
│  Web Client │◄────WebSocket──────►│    Proxy    │◄──────TCP─────────►│   Murmur    │
│  (Browser)  │    Binary/Protobuf  │    (Go)     │     Protobuf       │  (Server)   │
└─────────────┘                     └─────────────┘                     └─────────────┘
                                          │
                    ┌─────────────────────┼─────────────────────┐
                    │                     │                     │
                    ▼                     ▼                     ▼
              ┌──────────┐         ┌──────────┐         ┌──────────┐
              │ Metrics  │         │  Admin   │         │  Health  │
              │(Prometheus)│        │   API   │         │  Check   │
              └──────────┘         └──────────┘         └──────────┘
```

## 项目结构

```
proxy/
├── cmd/mumble-proxy/          # 应用入口
├── internal/
│   ├── admin/                 # 管理 API (83.7%)
│   ├── audit/                 # 审计日志 (88.0%)
│   ├── auth/                  # 认证模块 (90.8%)
│   ├── config/                # 配置管理 (87.9%)
│   ├── drain/                 # 连接排空 (95.6%)
│   ├── health/                # 健康检查 (97.7%)
│   ├── metrics/               # Prometheus 指标 (95.0%)
│   ├── middleware/            # HTTP 中间件 (94.6%)
│   ├── pool/                  # 连接池 (84.6%)
│   └── proxy/                 # 核心代理逻辑 (83.8%)
│       ├── websocket.go       # WebSocket 处理
│       ├── heartbeat.go       # 心跳机制
│       ├── retry.go           # 重连/熔断
│       ├── compression.go     # WebSocket 压缩
│       ├── reconnect.go       # 自动重连
│       ├── buffer.go          # Buffer 池化
│       └── ipfilter.go        # IP 过滤
├── docs/                      # 文档
│   ├── ARCHITECTURE.md        # 架构设计
│   ├── API.md                 # API 文档
│   ├── DEPLOYMENT.md          # 部署指南
│   ├── CONTRIBUTING.md        # 贡献指南
│   └── TROUBLESHOOTING.md     # 故障排查
├── test/
│   ├── e2e/                   # 端到端测试
│   └── integration/           # 集成测试
├── deployments/               # 部署配置
├── configs/                   # 配置文件
└── api/                       # OpenAPI 规范
```

## 文档

- [架构设计](docs/ARCHITECTURE.md)
- [API 文档](docs/API.md)
- [部署指南](docs/DEPLOYMENT.md)
- [贡献指南](docs/CONTRIBUTING.md)
- [故障排查](docs/TROUBLESHOOTING.md)

## 许可证

BSD 3-Clause License

## 贡献

欢迎提交 Issue 和 Pull Request！请阅读 [贡献指南](docs/CONTRIBUTING.md)。

## 相关项目

- [Mumble](https://www.mumble.info/) - 低延迟 VoIP 软件
- [mumble-web](https://github.com/johni0702/mumble-web) - Web 前端
- [mumble-client-codecs-browser](https://github.com/Johni0702/mumble-client-codecs-browser) - 浏览器编解码器