# API 文档

## 概述

Mumble WebSocket Proxy 提供以下 API 端点：

- WebSocket 代理端点
- 健康检查端点
- 管理 API
- 监控端点

## 基础信息

- **Base URL**: `http://host:8080`
- **管理端点认证**: `X-Admin-Token` Header
- **内容类型**: `application/json`

---

## WebSocket 端点

### 连接 Murmur 服务器

升级 HTTP 连接到 WebSocket 并代理到 Murmur 服务器。

```
GET /mumble
GET /mumble?token={token}
```

**参数**

| 参数 | 位置 | 类型 | 必填 | 描述 |
|------|------|------|------|------|
| token | query | string | 否 | 认证 Token，用于选择后端 |

**Headers**

| Header | 描述 |
|--------|------|
| X-Mumble-Token | 认证 Token (可选) |

**响应**

- `101 Switching Protocols` - 成功升级到 WebSocket
- `401 Unauthorized` - Token 认证失败
- `403 Forbidden` - IP 被阻止
- `503 Service Unavailable` - 后端不可用

**示例**

```javascript
const ws = new WebSocket('ws://localhost:8080/mumble?token=mytoken');
ws.onopen = () => console.log('Connected');
ws.onmessage = (e) => console.log('Message:', e.data);
ws.onerror = (e) => console.error('Error:', e);
```

---

## 健康检查端点

### 健康状态

返回服务整体健康状态。

```
GET /health
```

**响应**

```json
{
  "status": "healthy",
  "timestamp": "2024-01-15T10:30:00Z",
  "checks": {
    "murmur": {
      "status": "healthy",
      "latency_ms": 5
    },
    "memory": {
      "status": "healthy",
      "used_mb": 128,
      "total_mb": 1024
    }
  }
}
```

### 就绪状态

检查服务是否准备好接收流量。

```
GET /health/ready
```

**响应**

```json
{
  "ready": true,
  "checks": {
    "backends": 3,
    "active_connections": 42
  }
}
```

### 存活状态

Kubernetes 存活探针端点。

```
GET /health/live
```

**响应**

```json
{
  "alive": true
}
```

---

## 连接管理 API

### 列出连接

获取当前所有连接列表。

```
GET /api/connections
```

**参数**

| 参数 | 类型 | 默认值 | 描述 |
|------|------|--------|------|
| backend | string | - | 按后端过滤 |
| state | string | - | 按状态过滤 (active, idle, closing) |
| limit | int | 50 | 返回数量 (最大 500) |
| offset | int | 0 | 偏移量 |
| sort | string | id | 排序字段 (id, connected_at, last_activity, bytes_sent) |
| order | string | asc | 排序方向 (asc, desc) |

**响应**

```json
{
  "total": 100,
  "connections": [
    {
      "id": "conn-12345",
      "client_ip": "192.168.1.100",
      "backend": "server1",
      "user_id": "user-abc",
      "connected_at": "2024-01-15T10:00:00Z",
      "last_activity": "2024-01-15T10:30:00Z",
      "bytes_sent": 15000,
      "bytes_received": 8500,
      "messages_sent": 150,
      "messages_received": 85,
      "state": "active"
    }
  ],
  "offset": 0,
  "limit": 50
}
```

### 获取连接详情

获取单个连接的详细信息。

```
GET /api/connections/{id}
```

**参数**

| 参数 | 位置 | 类型 | 描述 |
|------|------|------|------|
| id | path | string | 连接 ID |

**响应**

```json
{
  "id": "conn-12345",
  "client_ip": "192.168.1.100",
  "backend": "server1",
  "user_id": "user-abc",
  "connected_at": "2024-01-15T10:00:00Z",
  "last_activity": "2024-01-15T10:30:00Z",
  "bytes_sent": 15000,
  "bytes_received": 8500,
  "messages_sent": 150,
  "messages_received": 85,
  "state": "active",
  "protocol": "1.4.0",
  "user_agent": "MumbleClient/1.4.0",
  "metadata": {
    "username": "Alice"
  }
}
```

**错误响应**

- `404 Not Found` - 连接不存在

### 连接统计

获取连接统计信息。

```
GET /api/connections/stats
```

**响应**

```json
{
  "total": 100,
  "by_backend": {
    "server1": 50,
    "server2": 30,
    "server3": 20
  },
  "by_state": {
    "active": 85,
    "idle": 10,
    "closing": 5
  },
  "updated_at": "2024-01-15T10:30:00Z"
}
```

---

## 后端管理 API

### 列出后端

获取所有配置的后端服务器。

```
GET /api/backends
```

**响应**

```json
{
  "backends": [
    {
      "name": "server1",
      "host": "mumble1.example.com",
      "port": 64738,
      "status": "healthy",
      "connections": 50,
      "latency_ms": 5
    },
    {
      "name": "server2",
      "host": "mumble2.example.com",
      "port": 64738,
      "status": "healthy",
      "connections": 30,
      "latency_ms": 12
    }
  ]
}
```

### 创建后端

动态添加新的后端服务器。

```
POST /api/backends
```

**请求体**

```json
{
  "name": "server3",
  "host": "mumble3.example.com",
  "port": 64738,
  "tokens": ["token-new"]
}
```

**响应**

```json
{
  "name": "server3",
  "host": "mumble3.example.com",
  "port": 64738,
  "status": "created"
}
```

**错误响应**

- `400 Bad Request` - 参数错误
- `409 Conflict` - 后端已存在

### 删除后端

删除后端服务器配置。

```
DELETE /api/backends/{name}
```

**参数**

| 参数 | 位置 | 类型 | 描述 |
|------|------|------|------|
| name | path | string | 后端名称 |

**响应**

```json
{
  "status": "deleted",
  "connections_migrated": 5
}
```

---

## 配置管理 API

### 重载配置

热重载配置文件。

```
POST /api/reload
```

**响应**

```json
{
  "status": "success",
  "changes": [
    "backends.server2.port: 64738 -> 64739",
    "auth.tokens: added 2, removed 1"
  ],
  "warnings": [
    "server.port change requires restart"
  ]
}
```

### 获取配置

获取当前运行配置。

```
GET /api/config
```

**响应**

```json
{
  "server": {
    "host": "0.0.0.0",
    "port": 8080
  },
  "backends": [...],
  "auth": {
    "enabled": true
  },
  "metrics": {
    "enabled": true,
    "port": 9090
  }
}
```

---

## 日志 API

### 获取日志

获取缓冲的日志条目。

```
GET /api/logs
```

**参数**

| 参数 | 类型 | 默认值 | 描述 |
|------|------|--------|------|
| level | string | - | 日志级别过滤 |
| limit | int | 100 | 返回数量 |

**响应**

```json
{
  "entries": [
    {
      "timestamp": "2024-01-15T10:30:00Z",
      "level": "info",
      "message": "Client connected",
      "fields": {
        "client_ip": "192.168.1.100",
        "backend": "server1"
      }
    }
  ],
  "count": 1
}
```

### 日志流

WebSocket 端点用于实时日志流。

```
GET /api/logs/stream
```

**参数**

| 参数 | 类型 | 描述 |
|------|------|------|
| levels | string | 逗号分隔的日志级别 (debug,info,warn,error) |
| sources | string | 逗号分隔的日志来源 |

**响应**

服务端持续发送日志条目：

```json
{
  "timestamp": "2024-01-15T10:30:00Z",
  "level": "info",
  "message": "Client connected",
  "source": "proxy",
  "fields": {}
}
```

**控制命令**

客户端可发送 JSON 命令：

```json
{"command": "set_levels", "levels": ["error", "warn"]}
{"command": "set_sources", "sources": ["proxy", "auth"]}
```

---

## 监控端点

### Prometheus 指标

```
GET /metrics
```

**可用指标**

| 指标 | 类型 | 描述 |
|------|------|------|
| `mumble_proxy_connections_active` | Gauge | 当前活跃连接数 |
| `mumble_proxy_connections_total` | Counter | 总连接数 |
| `mumble_proxy_connections_errors` | Counter | 连接错误数 |
| `mumble_proxy_bytes_sent_total` | Counter | 发送字节总数 |
| `mumble_proxy_bytes_received_total` | Counter | 接收字节总数 |
| `mumble_proxy_messages_sent_total` | Counter | 发送消息总数 |
| `mumble_proxy_messages_received_total` | Counter | 接收消息总数 |
| `mumble_proxy_connection_duration_seconds` | Histogram | 连接持续时间 |
| `mumble_proxy_backend_latency_seconds` | Histogram | 后端延迟 |
| `mumble_proxy_errors_total` | Counter | 错误总数 |
| `mumble_proxy_reconnects_total` | Counter | 重连次数 |

---

## 性能分析端点

需要 `X-Admin-Token` 认证。

### 索引页面

```
GET /debug/pprof/
```

### CPU 分析

```
GET /debug/pprof/profile?seconds=30
```

### 堆内存分析

```
GET /debug/pprof/heap?gc=1
```

### Goroutine 分析

```
GET /debug/pprof/goroutine
```

### 执行追踪

```
GET /debug/pprof/trace?seconds=5
```

### 其他端点

```
GET /debug/pprof/cmdline
GET /debug/pprof/symbol
GET /debug/pprof/threadcreate
GET /debug/pprof/block
GET /debug/pprof/mutex
```

---

## 错误响应

所有错误响应格式：

```json
{
  "error": {
    "code": "AUTH_FAILED",
    "message": "Invalid authentication token",
    "details": {
      "token_prefix": "abc"
    }
  }
}
```

**错误码**

| 代码 | HTTP 状态 | 描述 |
|------|-----------|------|
| `INVALID_REQUEST` | 400 | 请求参数错误 |
| `AUTH_FAILED` | 401 | 认证失败 |
| `FORBIDDEN` | 403 | 权限不足 |
| `NOT_FOUND` | 404 | 资源不存在 |
| `CONFLICT` | 409 | 资源冲突 |
| `RATE_LIMITED` | 429 | 请求过于频繁 |
| `INTERNAL_ERROR` | 500 | 内部错误 |
| `SERVICE_UNAVAILABLE` | 503 | 服务不可用 |

---

## OpenAPI 规范

完整的 OpenAPI 3.0 规范可在 `/api/openapi.yaml` 获取。

```bash
curl http://localhost:8080/api/openapi.yaml
```