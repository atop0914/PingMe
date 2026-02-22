# PingMe 部署指南

## 目录
- [快速开始](#快速开始)
- [环境要求](#环境要求)
- [配置说明](#配置说明)
- [Docker 部署](#docker-部署)
- [手动部署](#手动部署)
- [验证清单](#验证清单)
- [监控与告警](#监控与告警)
- [回滚方案](#回滚方案)
- [常见问题](#常见问题)

---

## 快速开始

### 1. 一键启动（推荐）

```bash
# 克隆项目
git clone https://github.com/atop0914/PingMe.git
cd PingMe

# 启动所有服务
docker-compose up -d

# 查看服务状态
docker-compose ps

# 查看日志
docker-compose logs -f pingme
```

### 2. 验证服务

```bash
# 健康检查
curl http://localhost:8080/health

# 版本信息
curl http://localhost:8080/version
```

服务启动成功后，访问以下地址：
- **API**: http://localhost:8080
- **Kafka UI**: http://localhost:8081 (可选，用于调试)

---

## 环境要求

### 最低配置
| 资源 | 最低要求 |
|------|----------|
| CPU | 2 核 |
| 内存 | 4 GB |
| 磁盘 | 20 GB |

### 推荐配置
| 资源 | 推荐配置 |
|------|----------|
| CPU | 4 核 |
| 内存 | 8 GB |
| 磁盘 | 50 GB SSD |

### 软件依赖
| 软件 | 版本 | 说明 |
|------|------|------|
| Docker | 20.10+ | 容器运行时 |
| Docker Compose | 2.0+ | 编排工具 |

---

## 配置说明

### 环境变量

| 变量 | 必填 | 默认值 | 说明 |
|------|------|--------|------|
| `APP_HOST` | 否 | 0.0.0.0 | 服务监听地址 |
| `APP_PORT` | 否 | 8080 | 服务监听端口 |
| `APP_ENV` | 否 | development | 运行环境 |
| `DB_HOST` | 是 | - | MySQL 地址 |
| `DB_PORT` | 是 | 3306 | MySQL 端口 |
| `DB_USERNAME` | 是 | root | MySQL 用户名 |
| `DB_PASSWORD` | 是 | - | MySQL 密码 |
| `DB_NAME` | 是 | pingme | 数据库名 |
| `REDIS_HOST` | 是 | - | Redis 地址 |
| `REDIS_PORT` | 是 | 6379 | Redis 端口 |
| `REDIS_PASSWORD` | 否 | - | Redis 密码 |
| `KAFKA_BROKERS` | 是 | - | Kafka 地址（逗号分隔） |
| `JWT_SECRET` | 是 | - | JWT 密钥（生产环境必须修改） |

### 配置文件

配置文件位于 `config/local.yml`，主要配置项：

```yaml
app:
  host: "0.0.0.0"
  port: 8080

database:
  host: "mysql"
  port: 3306
  username: "root"
  password: "root"
  name: "pingme"

redis:
  host: "redis"
  port: 6379

kafka:
  brokers:
    - "kafka:9092"
  consumer_group: "pingme-consumer"
  topic_prefix: "pingme"

jwt:
  secret: "your-jwt-secret-key-change-in-production"

websocket:
  ping_interval: 30
  pong_timeout: 60
  max_message_size: 8192
```

---

## Docker 部署

### 方式一：使用 Docker Compose（推荐）

#### 1. 启动所有服务

```bash
# 前台启动（查看日志）
docker-compose up

# 后台启动
docker-compose up -d

# 指定配置文件
docker-compose -f docker-compose.yml up -d
```

#### 2. 查看服务状态

```bash
# 查看所有容器
docker-compose ps

# 查看特定服务日志
docker-compose logs -f pingme
docker-compose logs -f kafka
docker-compose logs -f mysql
```

#### 3. 停止服务

```bash
# 停止所有服务
docker-compose down

# 停止并删除数据卷（谨慎！）
docker-compose down -v
```

### 方式二：手动构建镜像

```bash
# 构建镜像
docker build -t pingme:latest .

# 运行镜像
docker run -d \
  --name pingme \
  -p 8080:8080 \
  -e DB_HOST=localhost \
  -e DB_PORT=3306 \
  -e DB_USERNAME=root \
  -e DB_PASSWORD=secret \
  -e DB_NAME=pingme \
  -e REDIS_HOST=localhost \
  -e REDIS_PORT=6379 \
  -e KAFKA_BROKERS=localhost:9092 \
  -e JWT_SECRET=your-secret-key \
  pingme:latest
```

### 方式三：使用预构建镜像

```bash
# 从 GitHub Container Registry 拉取
docker pull ghcr.io/atop0914/pingme:latest

# 运行
docker run -d \
  --name pingme \
  -p 8080:8080 \
  -e KAFKA_BROKERS=your-kafka:9092 \
  -e REDIS_HOST=your-redis \
  -e DB_HOST=your-mysql \
  ghcr.io/atop0914/pingme:latest
```

---

## 手动部署

### 1. 安装依赖

```bash
# 安装 Go 1.23+
wget https://go.dev/dl/go1.23.6.linux-amd64.tar.gz
sudo tar -C /usr/local -xzf go1.23.6.linux-amd64.tar.gz

# 安装 Redis
sudo apt-get install redis-server

# 安装 MySQL
sudo apt-get install mysql-server

# 安装 Kafka（可选，使用 Docker 也可）
```

### 2. 编译项目

```bash
# 克隆项目
git clone https://github.com/atop0914/PingMe.git
cd PingMe

# 下载依赖
go mod download

# 编译
go build -o pingme-server ./cmd/server
```

### 3. 配置

```bash
# 复制配置文件
cp config/local.yml config/production.yml

# 编辑配置
vim config/production.yml
```

### 4. 启动服务

```bash
# 使用配置文件启动
APP_CONF=config/production.yml ./pingme-server

# 或使用环境变量
DB_HOST=localhost REDIS_HOST=localhost KAFKA_BROKERS=localhost:9092 ./pingme-server
```

### 5. 使用 Systemd（推荐用于生产）

```bash
# 创建服务文件
sudo vim /etc/systemd/system/pingme.service

# 内容：
# [Unit]
# Description=PingMe IM Server
# After=network.target

# [Service]
# Type=simple
# User=ubuntu
# WorkingDirectory=/opt/pingme
# ExecStart=/opt/pingme/pingme-server
# Environment=APP_ENV=production
# Restart=always
# RestartSec=10

# [Install]
# WantedBy=multi-user.target

# 启动服务
sudo systemctl enable pingme
sudo systemctl start pingme
sudo systemctl status pingme
```

---

## 验证清单

### 功能验证

- [ ] **健康检查**: `curl http://localhost:8080/health` 返回 `{"status":"ok"}`
- [ ] **版本信息**: `curl http://localhost:8080/version` 返回版本信息
- [ ] **用户注册**: POST `/api/v1/user/register` 可以创建新用户
- [ ] **用户登录**: POST `/api/v1/user/login` 返回 JWT token
- [ ] **WebSocket 连接**: 可以建立 WS 连接并完成鉴权
- [ ] **私聊消息**: 用户 A 可以发送消息给用户 B
- [ ] **群聊创建**: 可以创建群聊并添加成员
- [ ] **群聊消息**: 群内成员可以接收群消息
- [ ] **离线消息**: 重新登录后可以拉取离线消息
- [ ] **消息 ACK**: 客户端可以发送消息确认

### 性能验证

- [ ] **并发连接**: 支持 1000+ WebSocket 并发连接
- [ ] **消息延迟**: 端到端延迟 < 1 秒
- [ ] **Kafka 消费**: 消息可以正常生产/消费

### 稳定性验证

- [ ] **断线重连**: 网络断开后可以自动重连
- [ ] **服务重启**: 服务重启后数据不丢失
- [ ] **Kafka 重启**: Kafka 重启后服务恢复正常

---

## 监控与告警

### 日志查看

```bash
# 应用日志
docker-compose logs pingme

# 错误日志
docker-compose logs pingme | grep ERROR

# 实时跟踪
docker-compose logs -f --tail=100 pingme
```

### 监控指标

| 指标 | 说明 |
|------|------|
| `error_rate` | 错误率 |
| `kafka_consumer_lag` | Kafka 消费延迟 |
| `p99_latency_ms` | P99 延迟 |
| `active_connections` | 当前活跃连接数 |

### 告警配置

在 `config/local.yml` 中配置告警规则：

```yaml
alert:
  enabled: true
  interval: 30
  rules:
    - name: "error_rate_high"
      metric: "error_rate"
      condition: "gt"
      threshold: 0.05
      duration: 60
      severity: "critical"
      message: "错误率超过 5%"
```

---

## 回滚方案

### Docker 部署回滚

```bash
# 1. 停止当前服务
docker-compose down

# 2. 切换到上一个版本
git checkout <previous-version-tag>

# 3. 重新构建并启动
docker-compose up -d

# 4. 验证服务
curl http://localhost:8080/health
```

### 数据回滚

> ⚠️ 注意：数据回滚会丢失数据，请谨慎操作！

```bash
# 备份当前数据
docker-compose exec mysql mysqldump -u root -p pingme > backup_$(date +%Y%m%d).sql

# 恢复数据
docker-compose exec -T mysql -u root -p pingme < backup_20240101.sql
```

### 快速回滚脚本

```bash
#!/bin/bash
# rollback.sh

VERSION=${1:-"previous"}

echo "Rolling back to: $VERSION"

# Stop current service
docker-compose down

# Checkout version
git checkout $VERSION

# Rebuild and start
docker-compose build pingme
docker-compose up -d

# Wait for health check
sleep 10

# Verify
if curl -s http://localhost:8080/health | grep -q "ok"; then
    echo "Rollback successful!"
else
    echo "Rollback failed! Please check logs."
    docker-compose logs pingme
fi
```

---

## 常见问题

### Q1: 启动失败，提示 "connection refused"

**原因**: 依赖服务未启动或地址配置错误

**解决**:
```bash
# 检查依赖服务状态
docker-compose ps

# 检查配置中的地址是否正确
# config/local.yml 中的 host 应该使用服务名（docker-compose 网络内）
```

### Q2: Kafka 消费失败

**原因**: Kafka 未就绪或网络问题

**解决**:
```bash
# 检查 Kafka 状态
docker-compose logs kafka

# 检查 Kafka 主题
docker-compose exec kafka kafka-topics --list --bootstrap-server localhost:9092

# 手动创建主题（如果需要）
docker-compose exec kafka kafka-topics --create --topic pingme-messages --bootstrap-server localhost:9092 --partitions 3 --replication-factor 1
```

### Q3: WebSocket 连接失败

**原因**: 端口未开放或防火墙阻止

**解决**:
```bash
# 检查端口
netstat -tlnp | grep 8080

# 检查防火墙
sudo ufw allow 8080/tcp
```

### Q4: 内存使用过高

**原因**: 默认配置可能不适合小内存机器

**解决**: 调整 JVM/Kafka 内存或使用更小的 Docker Compose 配置

---

## 生产部署建议

### 1. 安全加固

- [ ] 修改默认 JWT 密钥
- [ ] 使用强数据库密码
- [ ] 启用 TLS/SSL
- [ ] 配置防火墙规则
- [ ] 限制容器资源

### 2. 高可用

- [ ] 部署多个 PingMe 实例（使用负载均衡）
- [ ] MySQL 主从复制
- [ ] Redis 哨兵/集群
- [ ] Kafka 多 broker 配置

### 3. 备份策略

- [ ] 定期备份数据库
- [ ] 定期备份配置文件
- [ ] 记录部署流程

---

## 许可证

MIT License - See [LICENSE](LICENSE) for details.
