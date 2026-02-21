package config

// AlertConfig 告警配置
type AlertConfig struct {
	Enabled  bool          `yaml:"enabled"`
	Interval int           `yaml:"interval"` // 告警检查间隔（秒）
	Rules    []AlertRule   `yaml:"rules"`
	Webhook  AlertWebhook  `yaml:"webhook"`
}

// AlertRule 告警规则
type AlertRule struct {
	Name      string `yaml:"name"`
	Metric    string `yaml:"metric"`    // 指标名称
	Condition string `yaml:"condition"` // 条件: gt, lt, eq, gte, lte
	Threshold any    `yaml:"threshold"` // 阈值
	Duration  int    `yaml:"duration"`  // 持续时间（秒）
	Severity  string `yaml:"severity"`  // 级别: critical, warning, info
	Message   string `yaml:"message"`   // 告警消息模板
}

// AlertWebhook 告警 webhook 配置
type AlertWebhook struct {
	URL      string   `yaml:"url"`
	Method   string   `yaml:"method"`   // GET, POST
	Headers  map[string]string `yaml:"headers"`
	Template string   `yaml:"template"` // 告警消息模板
}

// DefaultAlertConfig 返回默认告警配置
func DefaultAlertConfig() *AlertConfig {
	return &AlertConfig{
		Enabled:  false, // 默认关闭，需要时启用
		Interval: 30,
		Rules: []AlertRule{
			{
				Name:      "error_rate_high",
				Metric:    "error_rate",
				Condition: "gt",
				Threshold: 0.05, // 5% 错误率
				Duration:  60,
				Severity:  "critical",
				Message:   "错误率超过 5%，当前值: {{value}}",
			},
			{
				Name:      "kafka_lag_high",
				Metric:    "kafka_consumer_lag",
				Condition: "gt",
				Threshold: 1000,
				Duration:  120,
				Severity:  "warning",
				Message:   "Kafka 消息堆积超过 1000 条，当前值: {{value}}",
			},
			{
				Name:      "redis_connection_failed",
				Metric:    "redis_connected",
				Condition: "eq",
				Threshold: 0,
				Duration:  30,
				Severity:  "critical",
				Message:   "Redis 连接失败",
			},
			{
				Name:      "database_connection_failed",
				Metric:    "database_connected",
				Condition: "eq",
				Threshold: 0,
				Duration:  30,
				Severity:  "critical",
				Message:   "数据库连接失败",
			},
			{
				Name:      "latency_high",
				Metric:    "p99_latency_ms",
				Condition: "gt",
				Threshold: 1000, // 1秒
				Duration:  60,
				Severity:  "warning",
				Message:   "P99 延迟超过 1 秒，当前值: {{value}}ms",
			},
			{
				Name:      "connection_count_high",
				Metric:    "websocket_connections",
				Condition: "gt",
				Threshold: 10000,
				Duration:  60,
				Severity:  "warning",
				Message:   "WebSocket 连接数超过 10000，当前值: {{value}}",
			},
			{
				Name:      "memory_usage_high",
				Metric:    "memory_usage_percent",
				Condition: "gt",
				Threshold: 85,
				Duration:  120,
				Severity:  "warning",
				Message:   "内存使用率超过 85%，当前值: {{value}}%",
			},
			{
				Name:      "cpu_usage_high",
				Metric:    "cpu_usage_percent",
				Condition: "gt",
				Threshold: 80,
				Duration:  120,
				Severity:  "warning",
				Message:   "CPU 使用率超过 80%，当前值: {{value}}%",
			},
		},
		Webhook: AlertWebhook{
			Method: "POST",
			Template: `{
				"alert": "{{.Rule.Name}}",
				"severity": "{{.Rule.Severity}}",
				"message": "{{.Rule.Message}}",
				"value": {{.Value}},
				"timestamp": {{.Timestamp}},
				"instance": "{{.Instance}}"
			}`,
		},
	}
}
