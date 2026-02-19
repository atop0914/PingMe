package kafka

import (
	"encoding/json"
	"fmt"
	"sync"

	"PingMe/internal/logger"

	"github.com/IBM/sarama"
)

// MessageData 消息数据
type MessageData struct {
	MsgID          string `json:"msg_id"`
	ConversationID string `json:"conversation_id"`
	FromUserID     string `json:"from_user_id"`
	ToUserID       string `json:"to_user_id"`     // 单聊时使用
	GroupID        string `json:"group_id"`       // 群聊时使用
	Content        string `json:"content"`
	ContentType    string `json:"content_type"`
	ClientTS       int64  `json:"client_ts"`
	ServerTS       int64  `json:"server_ts"`
	Status         string `json:"status"`
	IsGroup        bool   `json:"is_group"` // 是否为群消息
}

// DeliveryEvent 投递事件
type DeliveryEvent struct {
	MsgID          string `json:"msg_id"`
	ConversationID string `json:"conversation_id"`
	FromUserID     string `json:"from_user_id"`
	ToUserID       string `json:"to_user_id"`
	Status         string `json:"status"` // delivered
	DeliveredAt    int64  `json:"delivered_at"`
}

// Producer Kafka 生产者
type Producer struct {
	producer     sarama.SyncProducer
	topic        string
	config       *Config
	mu           sync.RWMutex
	closed       bool
}

// NewProducer 创建 Kafka 生产者
func NewProducer(cfg *Config) (*Producer, error) {
	if len(cfg.Brokers) == 0 {
		return nil, fmt.Errorf("no Kafka brokers configured")
	}

	saramaConfig := sarama.NewConfig()
	saramaConfig.Producer.RequiredAcks = sarama.WaitForAll
	saramaConfig.Producer.Retry.Max = 3
	saramaConfig.Producer.Return.Successes = true
	saramaConfig.Producer.Partitioner = sarama.NewHashPartitioner

	producer, err := sarama.NewSyncProducer(cfg.Brokers, saramaConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kafka producer: %w", err)
	}

	p := &Producer{
		producer: producer,
		topic:    cfg.GetMessageTopic(),
		config:   cfg,
	}

	logger.Info("Kafka producer initialized",
		"brokers", cfg.Brokers,
		"topic", p.topic)

	return p, nil
}

// SendMessage 发送消息到 Kafka
func (p *Producer) SendMessage(msg *MessageData) error {
	p.mu.RLock()
	if p.closed {
		p.mu.RUnlock()
		return fmt.Errorf("producer is closed")
	}
	p.mu.RUnlock()

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	kafkaMsg := &sarama.ProducerMessage{
		Topic: p.topic,
		Key:   sarama.StringEncoder(msg.ConversationID), // key = conversationId 用于分区
		Value: sarama.ByteEncoder(data),
	}

	partition, offset, err := p.producer.SendMessage(kafkaMsg)
	if err != nil {
		logger.Error("Failed to send message to Kafka",
			"msg_id", msg.MsgID,
			"conversation_id", msg.ConversationID,
			"error", err)
		return fmt.Errorf("failed to send message: %w", err)
	}

	logger.Debug("Message sent to Kafka",
		"msg_id", msg.MsgID,
		"conversation_id", msg.ConversationID,
		"partition", partition,
		"offset", offset)

	return nil
}

// Close 关闭生产者
func (p *Producer) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return nil
	}

	p.closed = true
	if p.producer != nil {
		return p.producer.Close()
	}
	return nil
}

// IsClosed 检查生产者是否已关闭
func (p *Producer) IsClosed() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.closed
}
