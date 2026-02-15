package kafka

import (
	"fmt"

	"PingMe/internal/config"
)

// MessageTopic 消息主题
const MessageTopic = "pingme-messages"

// Config Kafka 配置
type Config struct {
	Brokers       []string
	ConsumerGroup string
	TopicPrefix   string
}

// NewConfig 从应用配置创建 Kafka 配置
func NewConfig(cfg *config.Config) *Config {
	return &Config{
		Brokers:       cfg.Kafka.Brokers,
		ConsumerGroup: cfg.Kafka.ConsumerGroup,
		TopicPrefix:   cfg.Kafka.TopicPrefix,
	}
}

// GetMessageTopic 获取消息主题全名
func (c *Config) GetMessageTopic() string {
	if c.TopicPrefix != "" {
		return fmt.Sprintf("%s-messages", c.TopicPrefix)
	}
	return MessageTopic
}
