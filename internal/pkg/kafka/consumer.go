package kafka

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"PingMe/internal/logger"
	"PingMe/internal/model/message"
	msgrepo "PingMe/internal/repository/message"

	"github.com/IBM/sarama"
)

// DeliveryEvent 投递事件
type DeliveryEvent struct {
	MsgID          string `json:"msg_id"`
	ConversationID string `json:"conversation_id"`
	FromUserID     string `json:"from_user_id"`
	ToUserID       string `json:"to_user_id"`
	Status         string `json:"status"` // delivered
	DeliveredAt    int64  `json:"delivered_at"`
}

// ConsumerHandler 消费者处理接口
type ConsumerHandler interface {
	// HandleMessage 处理消息（落库 + 生成投递事件）
	HandleMessage(ctx context.Context, msg *MessageData) error
	// HandleDeliveryEvent 处理投递事件
	HandleDeliveryEvent(event *DeliveryEvent) error
}

// Consumer Kafka 消费者
type Consumer struct {
	consumer    sarama.ConsumerGroup
	topic       string
	config      *Config
	handler     ConsumerHandler
	msgRepo     *msgrepo.Repository
	wg          sync.WaitGroup
	ctx         context.Context
	cancel      context.CancelFunc
	ready       chan bool
	closed      bool
	mu          sync.Mutex
}

// NewConsumer 创建 Kafka 消费者
func NewConsumer(cfg *Config, handler ConsumerHandler, msgRepo *msgrepo.Repository) (*Consumer, error) {
	if len(cfg.Brokers) == 0 {
		return nil, fmt.Errorf("no Kafka brokers configured")
	}

	saramaConfig := sarama.NewConfig()
	saramaConfig.Consumer.Group.Rebalance.GroupStrategies = []sarama.BalanceStrategy{sarama.NewBalanceStrategyRoundRobin()}
	saramaConfig.Consumer.Offsets.Initial = sarama.OffsetOldest

	consumer, err := sarama.NewConsumerGroup(cfg.Brokers, cfg.ConsumerGroup, saramaConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kafka consumer: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	c := &Consumer{
		consumer: consumer,
		topic:    cfg.GetMessageTopic(),
		config:   cfg,
		handler:  handler,
		msgRepo:  msgRepo,
		ctx:      ctx,
		cancel:   cancel,
		ready:    make(chan bool),
	}

	logger.Info("Kafka consumer initialized",
		"brokers", cfg.Brokers,
		"topic", c.topic,
		"consumer_group", cfg.ConsumerGroup)

	return c, nil
}

// Start 启动消费者
func (c *Consumer) Start() {
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		for {
			if err := c.consumer.Consume(c.ctx, []string{c.topic}, c); err != nil {
				logger.Error("Consumer error", "error", err)
			}
			if c.ctx.Err() != nil {
				return
			}
			c.ready = make(chan bool)
		}
	}()
}

// Stop 停止消费者
func (c *Consumer) Stop() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	c.mu.Unlock()

	c.cancel()
	c.wg.Wait()

	if c.consumer != nil {
		c.consumer.Close()
	}

	logger.Info("Kafka consumer stopped")
}

// Setup 在每个消费者开始消费前调用
func (c *Consumer) Setup(sarama.ConsumerGroupSession) error {
	close(c.ready)
	return nil
}

// Cleanup 在每个消费者结束消费后调用
func (c *Consumer) Cleanup(sarama.ConsumerGroupSession) error {
	return nil
}

// ConsumeClaim 消费消息
func (c *Consumer) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	for {
		select {
		case <-c.ctx.Done():
			return nil
		case msg, ok := <-claim.Messages():
			if !ok {
				return nil
			}

			// 解析消息
			var msgData MessageData
			if err := json.Unmarshal(msg.Value, &msgData); err != nil {
				logger.Warn("Failed to unmarshal message",
					"error", err,
					"offset", msg.Offset)
				session.MarkMessage(msg, "")
				continue
			}

			// 处理消息（落库 + 生成投递事件）
			if err := c.handler.HandleMessage(c.ctx, &msgData); err != nil {
				logger.Error("Failed to handle message",
					"msg_id", msgData.MsgID,
					"error", err)
				// 消息处理失败，不 ACK，触发重试
				continue
			}

			// 标记消息已处理
			session.MarkMessage(msg, "")
			logger.Debug("Message consumed and processed",
				"msg_id", msgData.MsgID,
				"offset", msg.Offset)
		}
	}
}

// KafkaMessageConsumer 实现 ConsumerHandler 接口
type KafkaMessageConsumer struct {
	msgRepo *msgrepo.Repository
	hub     interface {
		SendToUser(userID string, message []byte)
		GetUserConnections(userID string) interface{}
	}
}

// NewKafkaMessageConsumer 创建 Kafka 消息消费者
func NewKafkaMessageConsumer(msgRepo *msgrepo.Repository, hub interface {
	SendToUser(userID string, message []byte)
	GetUserConnections(userID string) interface{}
}) *KafkaMessageConsumer {
	return &KafkaMessageConsumer{
		msgRepo: msgRepo,
		hub:     hub,
	}
}

// HandleMessage 处理消息：落库 + 投递
func (h *KafkaMessageConsumer) HandleMessage(ctx context.Context, msg *MessageData) error {
	// 幂等检查：检查 msg_id 是否已存在
	existingMsg, err := h.msgRepo.GetMessageByMsgID(msg.MsgID)
	if err == nil && existingMsg != nil {
		// 消息已存在，跳过（重复消费）
		logger.Debug("Message already exists, skip (idempotent)",
			"msg_id", msg.MsgID)
		return nil
	}

	// 创建消息记录
	msgModel := &message.Message{
		MsgID:          msg.MsgID,
		ConversationID: msg.ConversationID,
		FromUserID:     msg.FromUserID,
		ToUserID:       msg.ToUserID,
		Content:        msg.Content,
		ContentType:    message.MessageType(msg.ContentType),
		Status:         message.MessageStatus(msg.Status),
		ClientTS:       msg.ClientTS,
		ServerTS:       msg.ServerTS,
	}

	// 保存到数据库
	if err := h.msgRepo.CreateMessage(msgModel); err != nil {
		logger.Error("Failed to save message to DB",
			"msg_id", msg.MsgID,
			"error", err)
		return fmt.Errorf("failed to create message: %w", err)
	}

	// 尝试实时投递
	delivered := h.deliverMessage(ctx, msg.ToUserID, msg)

	// 更新消息状态
	if delivered {
		msgModel.Status = message.MsgStatusDelivered
		h.msgRepo.UpdateMessageStatus(msg.MsgID, message.MsgStatusDelivered)
		
		// 生成投递事件
		h.generateDeliveryEvent(msg)
	}

	logger.Info("Message processed",
		"msg_id", msg.MsgID,
		"delivered", delivered)

	return nil
}

// deliverMessage 尝试实时投递消息
func (h *KafkaMessageConsumer) deliverMessage(ctx context.Context, toUserID string, msg *MessageData) bool {
	// 构建推送消息
	pushMsg := map[string]interface{}{
		"type": "message",
		"payload": map[string]interface{}{
			"msg_id":          msg.MsgID,
			"conversation_id": msg.ConversationID,
			"from_user_id":    msg.FromUserID,
			"content":         msg.Content,
			"content_type":    msg.ContentType,
			"status":          "delivered",
			"server_ts":       msg.ServerTS,
			"client_ts":       msg.ClientTS,
		},
	}
	data, _ := json.Marshal(pushMsg)

	// 尝试本地投递（通过 hub）
	if h.hub != nil {
		h.hub.SendToUser(toUserID, data)
	}

	// 检查用户是否在线（本地或 Redis）
	// 注意：这里简化处理，实际应该检查 Redis
	// 如果在线，返回 true；离线返回 false
	// 由于 Kafka Consumer 可能在不同实例运行，需要通过 Redis 查询

	// TODO: 检查用户在线状态
	// 这里简单返回 false，让消息保持发送状态，客户端通过拉取获取
	return false
}

// generateDeliveryEvent 生成投递事件
func (h *KafkaMessageConsumer) generateDeliveryEvent(msg *MessageData) {
	event := &DeliveryEvent{
		MsgID:          msg.MsgID,
		ConversationID: msg.ConversationID,
		FromUserID:     msg.FromUserID,
		ToUserID:       msg.ToUserID,
		Status:         "delivered",
		DeliveredAt:    time.Now().UnixMilli(),
	}

	logger.Debug("Delivery event generated",
		"msg_id", event.MsgID,
		"to_user_id", event.ToUserID,
		"delivered_at", event.DeliveredAt)
}

// HandleDeliveryEvent 处理投递事件（可扩展，用于推送通知等）
func (h *KafkaMessageConsumer) HandleDeliveryEvent(event *DeliveryEvent) error {
	logger.Info("Delivery event processed",
		"msg_id", event.MsgID,
		"to_user_id", event.ToUserID,
		"status", event.Status)
	return nil
}
