package kafka

import (
	"fmt"

	"PingMe/internal/config"
)

// MessageTopic 消息主题
const MessageTopic = "pingme-messages"

// DLQTopic 死信队列主题
const DLQTopic = "pingme-messages-dlq"

// Config Kafka 配置
type Config struct {
	Brokers       []string
	ConsumerGroup string
	TopicPrefix   string

	// 重试配置
	RetryMaxAttempts    int  // 最大重试次数 (默认 3)
	RetryInitialBackoff  int  // 初始退避时间 (毫秒, 默认 100ms)
	RetryMaxBackoff      int  // 最大退避时间 (毫秒, 默认 10s)
	RetryMultiplier      float64 // 退避倍数 (默认 2.0)

	// DLQ 配置
	EnableDLQ bool // 是否启用 DLQ
}

// NewConfig 从应用配置创建 Kafka 配置
func NewConfig(cfg *config.Config) *Config {
	return &Config{
		Brokers:            cfg.Kafka.Brokers,
		ConsumerGroup:      cfg.Kafka.ConsumerGroup,
		TopicPrefix:        cfg.Kafka.TopicPrefix,
		RetryMaxAttempts:   cfg.Kafka.RetryMaxAttempts,
		RetryInitialBackoff: cfg.Kafka.RetryInitialBackoff,
		RetryMaxBackoff:    cfg.Kafka.RetryMaxBackoff,
		RetryMultiplier:    cfg.Kafka.RetryMultiplier,
		EnableDLQ:          cfg.Kafka.EnableDLQ,
	}
}

// GetMessageTopic 获取消息主题全名
func (c *Config) GetMessageTopic() string {
	if c.TopicPrefix != "" {
		return fmt.Sprintf("%s-messages", c.TopicPrefix)
	}
	return MessageTopic
}

// GetDLQTopic 获取 DLQ 主题全名
func (c *Config) GetDLQTopic() string {
	if c.TopicPrefix != "" {
		return fmt.Sprintf("%s-messages-dlq", c.TopicPrefix)
	}
	return DLQTopic
}

// GetRetryMaxAttempts 获取最大重试次数
func (c *Config) GetRetryMaxAttempts() int {
	if c.RetryMaxAttempts <= 0 {
		return 3 // 默认最大重试 3 次
	}
	return c.RetryMaxAttempts
}

// GetRetryInitialBackoff 获取初始退避时间 (毫秒)
func (c *Config) GetRetryInitialBackoff() int {
	if c.RetryInitialBackoff <= 0 {
		return 100 // 默认 100ms
	}
	return c.RetryInitialBackoff
}

// GetRetryMaxBackoff 获取最大退避时间 (毫秒)
func (c *Config) GetRetryMaxBackoff() int {
	if c.RetryMaxBackoff <= 0 {
		return 10000 // 默认 10s
	}
	return c.RetryMaxBackoff
}

// GetRetryMultiplier 获取退避倍数
func (c *Config) GetRetryMultiplier() float64 {
	if c.RetryMultiplier <= 0 {
		return 2.0 // 默认 2 倍
	}
	return c.RetryMultiplier
}
