# Mumble WebSocket Proxy (Commercial Edition)

[![CI](https://github.com/mumble/mumble/workflows/CI/badge.svg)](https://github.com/mumble/mumble/actions)
[![Go Report Card](https://goreportcard.com/badge/github.com/mumble/mumble-next/proxy)](https://goreportcard.com/report/github.com/mumble/mumble-next/proxy)
[![License](https://img.shields.io/badge/License-BSD%203--Clause-blue.svg)](LICENSE)

WebSocket 到 TCP 的透明代理，让 Web 客户端可以连接 Murmur 服务器。

## 特性

### 核心功能
- **透明代理**: WebSocket ↔ TCP 双向数据转发
- **多租户支持**: 基于 Token 的后端路由
- **UDP 代理**: 可选的 UDP 音频代理

### 生产就绪
- **Prometheus 指标**: 连接数、字节数、延迟等
- **健康检查**: `/health`, `/healthz`, `/readyz`
- **优雅关闭**: 连接排空 (Drain) 支持
- **配置热重载**: SIGHUP 信号支持

### 安全性
- **Token 认证**: 支持多后端路由
- **连接限制**: 总连接数和每 IP 连接数限制
- **CORS 支持**: 可配置跨域策略

### 高性能
- **连接池**: Murmur 连接复用
- **零拷贝转发**: 高效数据传输
- **结构化日志**: JSON 格式输出

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
| http://localhost:8080/stats | 连接统计 |
| http://localhost:9090/metrics | Prometheus 指标 |

## 配置

### 配置文件 (YAML)

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

auth:
  enabled: false
  header: "X-Mumble-Token"
  query_param: "token"

backends:
  - name: "production"
    host: "mumble-prod.example.com"
    port: 64738
    tokens: ["prod-token-1", "prod-token-2"]

metrics:
  enabled: true
  port: 9090
  path: /metrics

logging:
  level: info
  format: json

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

### 配置热重载

```bash
# 发送 SIGHUP 信号重载配置
kill -HUP <pid>

# 或通过 API（计划中）
curl -X POST http://localhost:8080/admin/config/reload
```

## 多租户配置

支持基于 Token 的多后端路由：

```yaml
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
```

客户端连接：

```javascript
// URL 参数
const ws = new WebSocket('ws://localhost:8080/mumble?token=prod-token-1');

// HTTP Header
fetch('http://localhost:8080/mumble', {
  headers: { 'X-Mumble-Token': 'prod-token-1' }
});
```

## 管理 API

| 端点 | 方法 | 描述 |
|------|------|------|
| `/admin/backends` | GET/POST | 后端列表/创建 |
| `/admin/backends/:name` | GET/DELETE | 后端详情/删除 |
| `/admin/connections` | GET | 连接列表 |
| `/admin/config` | GET | 配置信息 |
| `/admin/drain` | POST | 开始排空 |
| `/admin/shutdown` | POST | 优雅关闭 |

## 监控

### Prometheus 指标

```
# HELP mumble_proxy_active_connections Current active connections
# TYPE mumble_proxy_active_connections gauge
mumble_proxy_active_connections 5

# HELP mumble_proxy_total_connections Total connections
# TYPE mumble_proxy_total_connections counter
mumble_proxy_total_connections 1234

# HELP mumble_proxy_bytes_transferred_total Bytes transferred
# TYPE mumble_proxy_bytes_transferred_total counter
mumble_proxy_bytes_transferred_total{direction="sent",backend="default"} 1048576

# HELP mumble_proxy_connection_duration_seconds Connection duration
# TYPE mumble_proxy_connection_duration_seconds histogram
mumble_proxy_connection_duration_seconds_bucket{le="1"} 10
```

### Grafana Dashboard

导入 `deployments/grafana/` 中的仪表板配置。

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

# 特定包
go test -v ./internal/proxy/...
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
│   ├── admin/                 # 管理 API
│   ├── auth/                  # Token 认证
│   ├── config/                # 配置管理
│   ├── drain/                 # 连接排空
│   ├── health/                # 健康检查
│   ├── metrics/               # Prometheus 指标
│   ├── middleware/            # HTTP 中间件
│   ├── pool/                  # 连接池
│   └── proxy/                 # 核心代理逻辑
├── pkg/api/                   # 公共 API
├── test/
│   ├── e2e/                   # 端到端测试
│   └── integration/           # 集成测试
├── deployments/               # 部署配置
├── configs/                   # 配置文件
└── api/                       # OpenAPI 规范
```

## 许可证

BSD 3-Clause License

## 贡献

欢迎提交 Issue 和 Pull Request！

## 相关项目

- [Mumble](https://www.mumble.info/) - 低延迟 VoIP 软件
- [mumble-web](https://github.com/johni0702/mumble-web) - Web 前端
- [mumble-client-codecs-browser](https://github.com/Johni0702/mumble-client-codecs-browser) - 浏览器编解码器