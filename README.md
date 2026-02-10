# PingMe - Instant Messaging System

A Go-based instant messaging system supporting private chats, group chats, offline messages, and online status management.

## Features

- **User Authentication**: JWT-based registration and login
- **Real-time Messaging**: WebSocket long connections
- **Private & Group Chats**: Comprehensive messaging capabilities
- **Offline Message Handling**: Message storage and retrieval
- **Online Status**: Redis-based presence management
- **Kafka Integration**: Asynchronous message processing

## Quick Start

### Prerequisites

- Go 1.23.6+
- Redis 7+
- Kafka 3.x
- MySQL 8+

### Environment Setup

1. Start dependencies using Docker:

```bash
docker run -d \
  --name redis \
  -p 6379:6379 \
  redis:7

docker run -d \
  --name mysql \
  -e MYSQL_ROOT_PASSWORD=root \
  -e MYSQL_DATABASE=im \
  -p 3306:3306 \
  mysql:8

docker run -d \
  --name zookeeper \
  -p 2181:2181 \
  confluentinc/cp-zookeeper:7.6.0

docker run -d \
  --name kafka \
  --link zookeeper:zookeeper \
  -e KAFKA_ZOOKEEPER_CONNECT=zookeeper:2181 \
  -e KAFKA_ADVERTISED_LISTENERS=PLAINTEXT://localhost:9092 \
  -e KAFKA_OFFSETS_TOPIC_REPLICATION_FACTOR=1 \
  -p 9092:9092 \
  confluentinc/cp-kafka:7.6.0
```

2. Create database tables:

```sql
CREATE DATABASE IF NOT EXISTS im;
```

### Configuration

Edit `config/local.yml` to configure your environment:

```yaml
app:
  host: "0.0.0.0"
  port: 8080
  name: "PingMe"
  env: "development"

database:
  host: "localhost"
  port: 3306
  username: "root"
  password: "root"
  name: "im"

redis:
  host: "localhost"
  port: 6379

kafka:
  brokers:
    - "localhost:9092"
```

### Build & Run

```bash
# Download dependencies
go mod download

# Build
go build -o pingme-server ./cmd/server

# Run
./pingme-server

# Or with custom config
APP_CONF=/path/to/config.yml ./pingme-server
```

### Environment Variables

Override configuration using environment variables:

| Variable | Description |
|----------|-------------|
| `APP_HOST` | Server host |
| `APP_PORT` | Server port |
| `APP_ENV` | Environment (development/production) |
| `DB_HOST` | MySQL host |
| `DB_PORT` | MySQL port |
| `DB_USERNAME` | MySQL username |
| `DB_PASSWORD` | MySQL password |
| `DB_NAME` | MySQL database name |
| `REDIS_HOST` | Redis host |
| `REDIS_PORT` | Redis port |
| `KAFKA_BROKERS` | Kafka brokers (comma-separated) |
| `JWT_SECRET` | JWT secret key |

## API Endpoints

### Health Check

```bash
GET /health
```

Returns service health status.

### Version Info

```bash
GET /version
```

Returns service version information.

## Project Structure

```
PingMe/
├── cmd/
│   └── server/           # Application entrypoint
├── internal/
│   ├── config/           # Configuration loading
│   ├── errorcode/        # Error code definitions
│   ├── handler/          # HTTP handlers
│   ├── logger/           # Logging utilities
│   └── middleware/       # HTTP middleware
├── pkg/
│   └── response/         # API response wrapper
├── config/               # Configuration files
├── go.mod
└── README.md
```

## Development

### Running Tests

```bash
go test ./...
```

### Code Style

```bash
# Format code
go fmt ./...

# Run linter
go vet ./...
```

## License

MIT License
