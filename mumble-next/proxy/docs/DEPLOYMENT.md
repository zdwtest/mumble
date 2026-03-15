# 部署指南

## 概述

本文档介绍 Mumble WebSocket Proxy 的各种部署方式。

## 系统要求

- **操作系统**: Linux, macOS, Windows
- **Go 版本**: 1.21+ (从源码构建)
- **内存**: 最小 128MB, 推荐 512MB+
- **CPU**: 最小 1 核, 推荐 2+ 核

## 部署方式

### 1. 二进制部署

#### 下载预编译二进制

```bash
# Linux amd64
curl -LO https://github.com/mumble/mumble/releases/download/v1.1.0/mumble-proxy-linux-amd64
chmod +x mumble-proxy-linux-amd64

# macOS arm64
curl -LO https://github.com/mumble/mumble/releases/download/v1.1.0/mumble-proxy-darwin-arm64
chmod +x mumble-proxy-darwin-arm64
```

#### 从源码构建

```bash
git clone https://github.com/mumble/mumble.git
cd mumble/mumble-next/proxy
make build

# 输出: bin/mumble-proxy
```

#### 运行

```bash
# 前台运行
./mumble-proxy --config config.yaml

# 后台运行
nohup ./mumble-proxy --config config.yaml > proxy.log 2>&1 &
```

### 2. Docker 部署

#### Dockerfile

```dockerfile
FROM golang:1.21-alpine AS builder

WORKDIR /app
COPY . .
RUN CGO_ENABLED=0 go build -o mumble-proxy ./cmd/mumble-proxy

FROM alpine:latest

RUN apk --no-cache add ca-certificates
WORKDIR /app
COPY --from=builder /app/mumble-proxy .
COPY config.yaml .

EXPOSE 8080 9090

ENTRYPOINT ["./mumble-proxy"]
CMD ["--config", "config.yaml"]
```

#### 构建镜像

```bash
docker build -t mumble-proxy:latest .
```

#### 运行容器

```bash
docker run -d \
  --name mumble-proxy \
  -p 8080:8080 \
  -p 9090:9090 \
  -v $(pwd)/config.yaml:/app/config.yaml \
  -v $(pwd)/logs:/app/logs \
  --restart unless-stopped \
  mumble-proxy:latest
```

#### Docker Compose

```yaml
version: '3.8'

services:
  mumble-proxy:
    image: mumble-proxy:latest
    container_name: mumble-proxy
    ports:
      - "8080:8080"
      - "9090:9090"
    volumes:
      - ./config.yaml:/app/config.yaml:ro
      - ./logs:/app/logs
      - ./certs:/app/certs:ro
    environment:
      - LOG_LEVEL=info
    restart: unless-stopped
    healthcheck:
      test: ["CMD", "wget", "-q", "--spider", "http://localhost:8080/health"]
      interval: 30s
      timeout: 10s
      retries: 3
    networks:
      - mumble-network

  prometheus:
    image: prom/prometheus:latest
    container_name: prometheus
    ports:
      - "9091:9090"
    volumes:
      - ./prometheus.yml:/etc/prometheus/prometheus.yml:ro
    command:
      - '--config.file=/etc/prometheus/prometheus.yml'
    networks:
      - mumble-network

  grafana:
    image: grafana/grafana:latest
    container_name: grafana
    ports:
      - "3000:3000"
    volumes:
      - grafana-data:/var/lib/grafana
    environment:
      - GF_SECURITY_ADMIN_PASSWORD=admin
    networks:
      - mumble-network

networks:
  mumble-network:
    driver: bridge

volumes:
  grafana-data:
```

### 3. Kubernetes 部署

#### ConfigMap

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: mumble-proxy-config
data:
  config.yaml: |
    server:
      host: "0.0.0.0"
      port: 8080

    murmur:
      host: "mumble-server"
      port: 64738

    metrics:
      enabled: true
      port: 9090

    logging:
      level: info
      format: json
```

#### Secret

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: mumble-proxy-secrets
type: Opaque
stringData:
  tls-cert: |
    -----BEGIN CERTIFICATE-----
    ...
    -----END CERTIFICATE-----
  tls-key: |
    -----BEGIN PRIVATE KEY-----
    ...
    -----END PRIVATE KEY-----
  admin-token: "your-admin-token"
```

#### Deployment

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: mumble-proxy
  labels:
    app: mumble-proxy
spec:
  replicas: 3
  selector:
    matchLabels:
      app: mumble-proxy
  template:
    metadata:
      labels:
        app: mumble-proxy
      annotations:
        prometheus.io/scrape: "true"
        prometheus.io/port: "9090"
        prometheus.io/path: "/metrics"
    spec:
      containers:
        - name: mumble-proxy
          image: mumble-proxy:latest
          imagePullPolicy: IfNotPresent
          ports:
            - containerPort: 8080
              name: http
            - containerPort: 9090
              name: metrics
          volumeMounts:
            - name: config
              mountPath: /app/config.yaml
              subPath: config.yaml
            - name: tls
              mountPath: /app/certs
              readOnly: true
          env:
            - name: ADMIN_TOKEN
              valueFrom:
                secretKeyRef:
                  name: mumble-proxy-secrets
                  key: admin-token
          resources:
            requests:
              cpu: 100m
              memory: 128Mi
            limits:
              cpu: 500m
              memory: 512Mi
          livenessProbe:
            httpGet:
              path: /health/live
              port: 8080
            initialDelaySeconds: 5
            periodSeconds: 10
          readinessProbe:
            httpGet:
              path: /health/ready
              port: 8080
            initialDelaySeconds: 5
            periodSeconds: 10
      volumes:
        - name: config
          configMap:
            name: mumble-proxy-config
        - name: tls
          secret:
            secretName: mumble-proxy-secrets
```

#### Service

```yaml
apiVersion: v1
kind: Service
metadata:
  name: mumble-proxy
spec:
  type: LoadBalancer
  selector:
    app: mumble-proxy
  ports:
    - port: 80
      targetPort: 8080
      name: http
    - port: 9090
      targetPort: 9090
      name: metrics
```

#### HorizontalPodAutoscaler

```yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: mumble-proxy-hpa
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: mumble-proxy
  minReplicas: 2
  maxReplicas: 10
  metrics:
    - type: Resource
      resource:
        name: cpu
        target:
          type: Utilization
          averageUtilization: 70
    - type: Resource
      resource:
        name: memory
        target:
          type: Utilization
          averageUtilization: 80
```

### 4. Systemd 服务

#### 服务文件

```ini
# /etc/systemd/system/mumble-proxy.service
[Unit]
Description=Mumble WebSocket Proxy
After=network.target

[Service]
Type=simple
User=mumble
Group=mumble
ExecStart=/usr/local/bin/mumble-proxy --config /etc/mumble-proxy/config.yaml
Restart=on-failure
RestartSec=5
LimitNOFILE=65536

# 安全设置
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/log/mumble-proxy

[Install]
WantedBy=multi-user.target
```

#### 启用服务

```bash
# 创建用户
sudo useradd -r -s /bin/false mumble

# 创建目录
sudo mkdir -p /etc/mumble-proxy /var/log/mumble-proxy
sudo chown mumble:mumble /var/log/mumble-proxy

# 安装服务
sudo cp mumble-proxy /usr/local/bin/
sudo cp config.yaml /etc/mumble-proxy/
sudo systemctl daemon-reload
sudo systemctl enable mumble-proxy
sudo systemctl start mumble-proxy
```

## 配置管理

### 环境变量

支持通过环境变量覆盖配置：

```bash
export MUMBLE_PROXY_SERVER_PORT=8080
export MUMBLE_PROXY_MURMUR_HOST=mumble.example.com
export MUMBLE_PROXY_METRICS_ENABLED=true
```

### 配置验证

```bash
./mumble-proxy --config config.yaml --validate
```

### 热重载

```bash
# API 方式
curl -X POST http://localhost:8080/api/reload \
  -H "X-Admin-Token: your-token"

# 信号方式
kill -HUP $(pidof mumble-proxy)
```

## TLS 配置

### 生成自签名证书

```bash
openssl req -x509 -newkey rsa:4096 -keyout key.pem -out cert.pem \
  -days 365 -nodes -subj "/CN=localhost"
```

### Let's Encrypt

```bash
# 使用 certbot
certbot certonly --standalone -d proxy.example.com

# 证书路径
# /etc/letsencrypt/live/proxy.example.com/fullchain.pem
# /etc/letsencrypt/live/proxy.example.com/privkey.pem
```

### 配置 TLS

```yaml
tls:
  enabled: true
  cert_file: /etc/letsencrypt/live/proxy.example.com/fullchain.pem
  key_file: /etc/letsencrypt/live/proxy.example.com/privkey.pem
  min_version: TLS1.2
```

## 监控配置

### Prometheus 配置

```yaml
# prometheus.yml
scrape_configs:
  - job_name: 'mumble-proxy'
    static_configs:
      - targets: ['mumble-proxy:9090']
    metrics_path: /metrics
```

### Grafana Dashboard

导入 dashboard JSON 文件或使用 ID: `XXXXX`

### 告警规则

```yaml
# alerts.yml
groups:
  - name: mumble-proxy
    rules:
      - alert: HighConnectionCount
        expr: mumble_proxy_connections_active > 1000
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: High connection count

      - alert: HighErrorRate
        expr: rate(mumble_proxy_errors_total[5m]) > 10
        for: 2m
        labels:
          severity: critical
        annotations:
          summary: High error rate
```

## 日志管理

### 日志轮转

```yaml
logging:
  level: info
  format: json
  output:
    type: file
    path: /var/log/mumble-proxy/proxy.log
    max_size: 100MB
    max_backups: 5
    max_age: 30d
    compress: true
```

### Logrotate 配置

```
# /etc/logrotate.d/mumble-proxy
/var/log/mumble-proxy/*.log {
    daily
    rotate 7
    compress
    delaycompress
    missingok
    notifempty
    create 0644 mumble mumble
    postrotate
        systemctl reload mumble-proxy > /dev/null 2>&1 || true
    endscript
}
```

## 性能调优

### 内核参数

```bash
# /etc/sysctl.d/99-mumble-proxy.conf
net.core.somaxconn = 65535
net.ipv4.tcp_max_syn_backlog = 65535
net.ipv4.tcp_tw_reuse = 1
net.ipv4.ip_local_port_range = 1024 65535
fs.file-max = 1048576
```

### 资源限制

```bash
# /etc/security/limits.d/mumble-proxy.conf
mumble soft nofile 65535
mumble hard nofile 65535
```

### 应用配置

```yaml
# 高并发配置
pool:
  max_idle: 200
  max_total: 2000
  idle_timeout: 5m

# 连接超时
server:
  read_timeout: 60s
  write_timeout: 60s

# 性能分析
pprof:
  enabled: true
  rate_limit: 30s
```

## 备份与恢复

### 配置备份

```bash
# 备份配置
tar -czf mumble-proxy-config-$(date +%Y%m%d).tar.gz \
  /etc/mumble-proxy/ \
  /etc/letsencrypt/live/proxy.example.com/
```

### 恢复

```bash
# 恢复配置
tar -xzf mumble-proxy-config-20240115.tar.gz -C /
systemctl restart mumble-proxy
```

## 故障排查

### 检查服务状态

```bash
# Systemd
systemctl status mumble-proxy
journalctl -u mumble-proxy -f

# Docker
docker logs mumble-proxy -f

# Kubernetes
kubectl logs -f deployment/mumble-proxy
```

### 健康检查

```bash
curl http://localhost:8080/health
curl http://localhost:8080/health/ready
curl http://localhost:8080/health/live
```

### 连接问题

```bash
# 检查端口监听
ss -tlnp | grep 8080

# 检查防火墙
iptables -L -n | grep 8080

# 测试后端连接
nc -zv mumble.example.com 64738
```