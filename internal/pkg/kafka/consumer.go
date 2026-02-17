package kafka

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"PingMe/internal/errorcode"
	"PingMe/internal/logger"
	"PingMe/internal/model/message"
	msgrepo "PingMe/internal/repository/message"

	"github.com/IBM/sarama"
)

// DLQMessage 死信队列消息结构
type DLQMessage struct {
	OriginalMsg   MessageData `json:"original_msg"`
	Error         string       `json:"error"`
	RetryCount    int          `json:"retry_count"`
	FailedAt      int64        `json:"failed_at"`
	ErrorCode     string       `json:"error_code"`
}

// ConsumerHandler 消费者处理接口
type ConsumerHandler interface {
	// HandleMessage 处理消息（落库 + 生成投递事件）
	HandleMessage(ctx context.Context, msg *MessageData) error
	// HandleDeliveryEvent 处理投递事件
	HandleDeliveryEvent(event *DeliveryEvent) error
}

// RetryConfig 重试配置
type RetryConfig struct {
	MaxAttempts   int           // 最大重试次数
	InitialDelay  time.Duration // 初始延迟
	MaxDelay      time.Duration // 最大延迟
	Multiplier    float64       // 延迟倍数
}

// DefaultRetryConfig 默认重试配置
func DefaultRetryConfig() *RetryConfig {
	return &RetryConfig{
		MaxAttempts:  3,
		InitialDelay: 100 * time.Millisecond,
		MaxDelay:     10 * time.Second,
		Multiplier:   2.0,
	}
}

// CalculateDelay 计算重试延迟（指数退避）
func (r *RetryConfig) CalculateDelay(retryCount int) time.Duration {
	delay := r.InitialDelay
	for i := 0; i < retryCount; i++ {
		delay = time.Duration(float64(delay) * r.Multiplier)
		if delay > r.MaxDelay {
			delay = r.MaxDelay
		}
	}
	return delay
}

// Consumer Kafka 消费者
type Consumer struct {
	consumer    sarama.ConsumerGroup
	topic       string
	dlqTopic    string
	config      *Config
	retryConfig *RetryConfig
	handler     ConsumerHandler
	msgRepo     *msgrepo.Repository
	dlqProducer sarama.SyncProducer
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

	// 初始化 DLQ Producer
	var dlqProducer sarama.SyncProducer
	if cfg.EnableDLQ {
		dlqConfig := sarama.NewConfig()
		dlqConfig.Producer.RequiredAcks = sarama.WaitForAll
		dlqConfig.Producer.Retry.Max = 3
		dlqProducer, err = sarama.NewSyncProducer(cfg.Brokers, dlqConfig)
		if err != nil {
			logger.Warn("Failed to create DLQ producer, DLQ disabled", "error", err)
			cfg.EnableDLQ = false
		}
	}

	ctx, cancel := context.WithCancel(context.Background())

	// 构建重试配置
	retryConfig := &RetryConfig{
		MaxAttempts:  cfg.GetRetryMaxAttempts(),
		InitialDelay: time.Duration(cfg.GetRetryInitialBackoff()) * time.Millisecond,
		MaxDelay:     time.Duration(cfg.GetRetryMaxBackoff()) * time.Millisecond,
		Multiplier:   cfg.GetRetryMultiplier(),
	}

	c := &Consumer{
		consumer:    consumer,
		topic:      cfg.GetMessageTopic(),
		dlqTopic:   cfg.GetDLQTopic(),
		config:     cfg,
		retryConfig: retryConfig,
		handler:    handler,
		msgRepo:    msgRepo,
		dlqProducer: dlqProducer,
		ctx:        ctx,
		cancel:     cancel,
		ready:      make(chan bool),
	}

	logger.Info("Kafka consumer initialized",
		"brokers", cfg.Brokers,
		"topic", c.topic,
		"dlq_topic", c.dlqTopic,
		"dlq_enabled", cfg.EnableDLQ,
		"consumer_group", cfg.ConsumerGroup,
		"retry_max_attempts", retryConfig.MaxAttempts)

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

	if c.dlqProducer != nil {
		c.dlqProducer.Close()
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
				logger.Error("Failed to unmarshal message",
					"error", err,
					"offset", msg.Offset,
					"error_code", errorcode.KafkaMessageUnmarshalError.Code)
				// 解析失败，不 ACK，消息会被重复消费
				// 这里我们可以选择发送 到 DLQ 或者直接跳过
				c.sendToDLQ(&msgData, err.Error(), 0, errorcode.KafkaMessageUnmarshalError.Code)
				session.MarkMessage(msg, "")
				continue
			}

			// 处理消息（带重试）
			retryCount := 0
			maxRetries := c.retryConfig.MaxAttempts

			for retryCount <= maxRetries {
				err := c.handler.HandleMessage(c.ctx, &msgData)
				
				if err == nil {
					// 处理成功，标记消息已处理
					session.MarkMessage(msg, "")
					logger.Info("Message consumed and processed",
						"msg_id", msgData.MsgID,
						"offset", msg.Offset,
						"retry_count", retryCount)
					break
				}

				retryCount++
				
				logger.Warn("Message processing failed, will retry",
					"msg_id", msgData.MsgID,
					"error", err,
					"retry_count", retryCount,
					"max_retries", maxRetries)

				if retryCount <= maxRetries {
					// 计算退避时间
					delay := c.retryConfig.CalculateDelay(retryCount - 1)
					logger.Info("Retrying after delay",
						"msg_id", msgData.MsgID,
						"delay", delay,
						"retry_count", retryCount)
					
					select {
					case <-c.ctx.Done():
						return nil
					case <-time.After(delay):
						// 继续重试
					}
				} else {
					// 重试次数耗尽
					logger.Error("Message processing failed after all retries, sending to DLQ",
						"msg_id", msgData.MsgID,
						"error", err,
						"error_code", errorcode.KafkaRetryExhausted.Code)

					// 发送到 DLQ
					c.sendToDLQ(&msgData, err.Error(), retryCount, errorcode.KafkaRetryExhausted.Code)
					
					// 标记消息已处理（避免无限重试）
					session.MarkMessage(msg, "")
				}
			}
		}
	}
}

// sendToDLQ 发送消息到死信队列
func (c *Consumer) sendToDLQ(msg *MessageData, errorMsg string, retryCount int, errorCode string) {
	if !c.config.EnableDLQ || c.dlqProducer == nil {
		logger.Debug("DLQ disabled, skipping",
			"msg_id", msg.MsgID)
		return
	}

	dlqMsg := DLQMessage{
		OriginalMsg: *msg,
		Error:       errorMsg,
		RetryCount:  retryCount,
		FailedAt:    time.Now().UnixMilli(),
		ErrorCode:   errorCode,
	}

	value, err := json.Marshal(dlqMsg)
	if err != nil {
		logger.Error("Failed to marshal DLQ message",
			"msg_id", msg.MsgID,
			"error", err)
		return
	}

	kafkaMsg := &sarama.ProducerMessage{
		Topic: c.dlqTopic,
		Key:   sarama.StringEncoder(msg.MsgID),
		Value: sarama.ByteEncoder(value),
	}

	partition, offset, err := c.dlqProducer.SendMessage(kafkaMsg)
	if err != nil {
		logger.Error("Failed to send message to DLQ",
			"msg_id", msg.MsgID,
			"error", err,
			"error_code", errorcode.KafkaDLQSendError.Code)
		return
	}

	logger.Info("Message sent to DLQ",
		"msg_id", msg.MsgID,
		"partition", partition,
		"offset", offset,
		"dlq_topic", c.dlqTopic,
		"retry_count", retryCount,
		"error_code", errorCode)
}

// KafkaMessageConsumer 实现 ConsumerHandler 接口
type KafkaMessageConsumer struct {
	msgRepo *msgrepo.Repository
	hub     interface {
		SendToUser(userID string, message []byte)
	}
}

// NewKafkaMessageConsumer 创建 Kafka 消息消费者
func NewKafkaMessageConsumer(msgRepo *msgrepo.Repository, hub interface {
	SendToUser(userID string, message []byte)
}) *KafkaMessageConsumer {
	return &KafkaMessageConsumer{
		msgRepo: msgRepo,
		hub:     hub,
	}
}

// HandleMessage 处理消息：落库 + 投递
// 消息投递语义: at-least-once + 幂等
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
			"error", err,
			"error_code", errorcode.KafkaMessagePersistError.Code)
		return fmt.Errorf("failed to create message: %w (code: %s)", err, errorcode.KafkaMessagePersistError.Code)
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
		"delivered", delivered,
		"delivery_semantic", "at-least-once")

	return nil
}

// deliverMessage 尝试实时投递消息
// 返回 true 表示投递成功，false 表示用户离线
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
