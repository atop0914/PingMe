// Package metrics provides Prometheus metrics for PingMe
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// HTTP metrics
	HTTPRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "pingme_http_requests_total",
			Help: "Total number of HTTP requests",
		},
		[]string{"method", "path", "status"},
	)

	HTTPRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "pingme_http_request_duration_seconds",
			Help:    "HTTP request duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "path"},
	)

	// WebSocket metrics
	WebSocketConnections = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "pingme_ws_connections_active",
			Help: "Number of active WebSocket connections",
		},
	)

	WebSocketMessagesTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "pingme_ws_messages_total",
			Help: "Total number of WebSocket messages",
		},
		[]string{"direction"}, // "send" or "receive"
	)

	// Message metrics
	MessagesSentTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "pingme_messages_sent_total",
			Help: "Total number of messages sent",
		},
	)

	MessagesReceivedTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "pingme_messages_received_total",
			Help: "Total number of messages received",
		},
	)

	MessagesFailedTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "pingme_messages_failed_total",
			Help: "Total number of messages failed",
		},
	)

	// Kafka metrics
	KafkaMessagesProduced = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "pingme_kafka_messages_produced_total",
			Help: "Total number of messages produced to Kafka",
		},
	)

	KafkaMessagesConsumed = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "pingme_kafka_messages_consumed_total",
			Help: "Total number of messages consumed from Kafka",
		},
	)

	KafkaProduceFailures = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "pingme_kafka_produce_failures_total",
			Help: "Total number of Kafka produce failures",
		},
	)

	// Database metrics
	DatabaseQueryDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "pingme_db_query_duration_seconds",
			Help:    "Database query duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"query_type"},
	)

	DatabaseQueryErrors = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "pingme_db_query_errors_total",
			Help: "Total number of database query errors",
		},
		[]string{"query_type"},
	)

	// Redis metrics
	RedisOperationDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "pingme_redis_operation_duration_seconds",
			Help:    "Redis operation duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"operation"},
	)

	RedisOperationErrors = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "pingme_redis_operation_errors_total",
			Help: "Total number of Redis operation errors",
		},
		[]string{"operation"},
	)

	// Connection pool metrics
	DatabaseConnectionsActive = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "pingme_db_connections_active",
			Help: "Number of active database connections",
		},
	)

	RedisConnectionsActive = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "pingme_redis_connections_active",
			Help: "Number of active Redis connections",
		},
	)

	// Business metrics
	OnlineUsers = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "pingme_online_users",
			Help: "Number of online users",
		},
	)

	ActiveConversations = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "pingme_active_conversations",
			Help: "Number of active conversations",
		},
	)
)
