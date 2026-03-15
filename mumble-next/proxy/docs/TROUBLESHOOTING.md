# 故障排查指南

## 常见问题

### 连接问题

#### 问题: 无法连接到代理服务器

**症状**
- 浏览器显示 "Connection refused"
- WebSocket 连接失败

**排查步骤**

1. 检查服务是否运行
```bash
# Linux
systemctl status mumble-proxy

# Docker
docker ps | grep mumble-proxy

# Kubernetes
kubectl get pods | grep mumble-proxy
```

2. 检查端口监听
```bash
ss -tlnp | grep 8080
netstat -an | grep 8080
```

3. 检查防火墙
```bash
# Linux (firewalld)
firewall-cmd --list-ports

# Linux (iptables)
iptables -L -n | grep 8080

# Linux (ufw)
ufw status
```

4. 检查日志
```bash
# Systemd
journalctl -u mumble-proxy -n 100

# Docker
docker logs mumble-proxy --tail 100
```

#### 问题: 无法连接到 Murmur 服务器

**症状**
- 代理返回 503 错误
- 日志显示 "connection refused" 到后端

**排查步骤**

1. 测试 Murmur 连通性
```bash
# TCP 测试
nc -zv mumble.example.com 64738

# Telnet 测试
telnet mumble.example.com 64738
```

2. 检查 Murmur 服务状态
```bash
# Systemd
systemctl status murmur

# 查看 Murmur 日志
journalctl -u murmur -n 50
```

3. 检查网络路由
```bash
# 路由追踪
traceroute mumble.example.com

# DNS 解析
nslookup mumble.example.com
dig mumble.example.com
```

---

### 认证问题

#### 问题: Token 认证失败

**症状**
- 返回 401 Unauthorized
- 日志显示 "invalid token"

**排查步骤**

1. 验证 Token 配置
```bash
# 查看当前后端配置
curl http://localhost:8080/api/backends

# 检查 Token 列表
curl http://localhost:8080/api/config | jq '.backends[].tokens'
```

2. 检查请求格式
```bash
# 方式1: URL 参数
curl -v "http://localhost:8080/mumble?token=your-token"

# 方式2: Header
curl -v -H "X-Mumble-Token: your-token" \
  "http://localhost:8080/mumble"
```

3. 检查日志
```bash
# 查看认证日志
curl "http://localhost:8080/api/logs?level=info" | jq '.entries[] | select(.type == "auth_failure")'
```

#### 问题: TLS 证书验证失败

**症状**
- 返回 403 Forbidden
- 日志显示 "certificate verify failed"

**排查步骤**

1. 检查证书链
```bash
# 验证证书
openssl verify -CAfile ca.pem client-cert.pem

# 查看证书信息
openssl x509 -in client-cert.pem -text -noout
```

2. 检查证书过期
```bash
openssl x509 -in client-cert.pem -noout -dates
```

3. 检查 CA 配置
```yaml
# config.yaml
tls:
  ca_file: /path/to/ca.pem  # 确认路径正确
  client_auth: require-and-verify
```

---

### 性能问题

#### 问题: CPU 使用率高

**症状**
- CPU 使用率持续偏高
- 响应变慢

**排查步骤**

1. 启用性能分析
```bash
# CPU 分析 (30秒)
curl -H "X-Admin-Token: your-token" \
  "http://localhost:8080/debug/pprof/profile?seconds=30" \
  -o cpu.prof

# 分析
go tool pprof -http=:8081 cpu.prof
```

2. 查看 Goroutine
```bash
# 获取 goroutine 数量
curl -H "X-Admin-Token: your-token" \
  "http://localhost:8080/debug/pprof/goroutine?debug=1"
```

3. 检查连接数
```bash
curl http://localhost:8080/api/connections/stats
```

#### 问题: 内存使用持续增长

**症状**
- 内存使用不断上升
- 可能 OOM

**排查步骤**

1. 堆内存分析
```bash
# 获取堆内存快照
curl -H "X-Admin-Token: your-token" \
  "http://localhost:8080/debug/pprof/heap" \
  -o heap.prof

# 分析
go tool pprof -http=:8081 heap.prof

# 查看分配对象
go tool pprof -alloc_objects heap.prof
```

2. 检查连接泄漏
```bash
# 查看活跃连接
curl http://localhost:8080/api/connections?state=active

# 检查长时间连接
curl http://localhost:8080/api/connections?sort=connected_at&order=asc
```

3. 检查缓冲区
```bash
# 查看压缩统计
curl http://localhost:8080/api/stats/compression
```

#### 问题: 响应延迟高

**症状**
- 客户端延迟明显
- 操作超时

**排查步骤**

1. 检查后端延迟
```bash
# Prometheus 查询
rate(mumble_proxy_backend_latency_seconds_sum[5m]) /
rate(mumble_proxy_backend_latency_seconds_count[5m])
```

2. 检查连接池状态
```bash
curl http://localhost:8080/api/pool/stats
```

3. 检查网络
```bash
# 到后端的延迟
ping mumble.example.com
mtr mumble.example.com
```

---

### 配置问题

#### 问题: 配置热重载失败

**症状**
- 配置更改不生效
- 返回错误

**排查步骤**

1. 验证配置文件
```bash
./mumble-proxy --config config.yaml --validate
```

2. 手动重载
```bash
# API 方式
curl -X POST -H "X-Admin-Token: your-token" \
  http://localhost:8080/api/reload

# 信号方式
kill -HUP $(pidof mumble-proxy)
```

3. 查看重载日志
```bash
curl "http://localhost:8080/api/logs" | jq '.entries[] | select(.message | contains("reload"))'
```

#### 问题: 配置项不生效

**症状**
- 配置已修改但行为未变

**检查点**

1. 确认配置优先级
   - 命令行参数 > 环境变量 > 配置文件

2. 检查是否需要重启
```yaml
# 这些配置需要重启
server:
  host: "0.0.0.0"  # 需要重启
  port: 8080        # 需要重启

tls:
  cert_file: ...    # 需要重启

metrics:
  port: 9090        # 需要重启
```

---

### 日志问题

#### 问题: 日志不输出

**检查点**

1. 确认日志级别
```yaml
logging:
  level: debug  # info, warn, error
```

2. 确认输出目标
```yaml
logging:
  output:
    type: file  # 或 stdout
    path: /var/log/mumble-proxy/proxy.log
```

3. 检查文件权限
```bash
ls -la /var/log/mumble-proxy/
```

#### 问题: 日志文件过大

**解决方案**

1. 配置日志轮转
```yaml
logging:
  output:
    max_size: 100MB
    max_backups: 5
    max_age: 30d
    compress: true
```

2. 使用 logrotate
```bash
# /etc/logrotate.d/mumble-proxy
/var/log/mumble-proxy/*.log {
    daily
    rotate 7
    compress
    missingok
    notifempty
}
```

---

## 诊断工具

### 健康检查脚本

```bash
#!/bin/bash
# health-check.sh

ENDPOINT="http://localhost:8080"

echo "=== Health Check ==="
curl -s "$ENDPOINT/health" | jq .

echo -e "\n=== Ready Check ==="
curl -s "$ENDPOINT/health/ready" | jq .

echo -e "\n=== Connection Stats ==="
curl -s "$ENDPOINT/api/connections/stats" | jq .

echo -e "\n=== Backend Status ==="
curl -s "$ENDPOINT/api/backends" | jq .
```

### 日志分析脚本

```bash
#!/bin/bash
# analyze-logs.sh

LOG_FILE="/var/log/mumble-proxy/proxy.log"

echo "=== Error Count by Type ==="
jq -r 'select(.level == "error") | .message' $LOG_FILE | sort | uniq -c | sort -rn

echo -e "\n=== Recent Errors ==="
jq -r 'select(.level == "error") | "\(.timestamp) \(.message)"' $LOG_FILE | tail -20

echo -e "\n=== Connection Errors ==="
jq -r 'select(.type == "connect") | select(.result == "failure")' $LOG_FILE | tail -10
```

### 性能监控脚本

```bash
#!/bin/bash
# monitor.sh

while true; do
    echo "=== $(date) ==="

    # 连接数
    CONNS=$(curl -s http://localhost:8080/api/connections/stats | jq '.total')
    echo "Active Connections: $CONNS"

    # 内存
    MEM=$(ps aux | grep mumble-proxy | grep -v grep | awk '{print $6}')
    echo "Memory (KB): $MEM"

    # CPU
    CPU=$(ps aux | grep mumble-proxy | grep -v grep | awk '{print $3}')
    echo "CPU (%): $CPU"

    echo "---"
    sleep 5
done
```

---

## 常用命令

```bash
# 查看服务状态
systemctl status mumble-proxy

# 重启服务
systemctl restart mumble-proxy

# 查看日志
journalctl -u mumble-proxy -f

# 检查端口
ss -tlnp | grep 8080

# 测试连接
curl -v http://localhost:8080/health

# 查看指标
curl http://localhost:9090/metrics | grep mumble_proxy

# 性能分析
go tool pprof http://localhost:8080/debug/pprof/profile?seconds=30

# 查看堆内存
go tool pprof http://localhost:8080/debug/pprof/heap

# 查看 goroutine
curl http://localhost:8080/debug/pprof/goroutine?debug=1
```

---

## 获取支持

如果以上步骤无法解决问题：

1. 收集诊断信息
   ```bash
   # 收集诊断数据
   curl http://localhost:8080/health > diagnostics-health.json
   curl http://localhost:8080/api/config > diagnostics-config.json
   curl http://localhost:8080/api/connections/stats > diagnostics-stats.json
   journalctl -u mumble-proxy -n 500 > diagnostics-logs.txt
   ```

2. 创建 GitHub Issue
   - 附上诊断信息
   - 描述复现步骤
   - 说明预期行为和实际行为

3. 社区支持
   - GitHub Discussions
   - Mumble Forums