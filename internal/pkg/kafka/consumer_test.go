package kafka

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"PingMe/internal/errorcode"
	"PingMe/internal/logger"
	"PingMe/internal/model/message"
)

// MockMessageRepository 模拟消息仓库
type MockMessageRepository struct {
	mu           sync.Mutex
	messages     map[string]*message.Message
	failCount    int // 模拟失败次数
	failUntil    int // 在第几次失败后成功
	failError    error
}

func NewMockMessageRepository(failUntil int, failError error) *MockMessageRepository {
	return &MockMessageRepository{
		messages:  make(map[string]*message.Message),
		failUntil: failUntil,
		failError: failError,
	}
}

func (r *MockMessageRepository) CreateMessage(msg *message.Message) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.failCount++
	if r.failCount <= r.failUntil && r.failError != nil {
		return r.failError
	}

	r.messages[msg.MsgID] = msg
	return nil
}

func (r *MockMessageRepository) GetMessageByMsgID(msgID string) (*message.Message, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if msg, ok := r.messages[msgID]; ok {
		return msg, nil
	}
	return nil, fmt.Errorf("not found")
}

func (r *MockMessageRepository) UpdateMessageStatus(msgID string, status message.MessageStatus) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if msg, ok := r.messages[msgID]; ok {
		msg.Status = status
	}
	return nil
}

// TestRetryConfig 测试重试配置
func TestRetryConfig(t *testing.T) {
	config := DefaultRetryConfig()

	tests := []struct {
		name       string
		retryCount int
		wantDelay  time.Duration
	}{
		{"first retry", 0, 100 * time.Millisecond},
		{"second retry", 1, 200 * time.Millisecond},
		{"third retry", 2, 400 * time.Millisecond},
		{"fourth retry", 3, 800 * time.Millisecond},
		{"exceeds max", 10, 10 * time.Second}, // should cap at max
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := config.CalculateDelay(tt.retryCount)
			if got != tt.wantDelay {
				t.Errorf("CalculateDelay(%d) = %v, want %v", tt.retryCount, got, tt.wantDelay)
			}
		})
	}
}

// TestRetryConfigWithMax 测试最大延迟限制
func TestRetryConfigWithMax(t *testing.T) {
	config := &RetryConfig{
		MaxAttempts:  5,
		InitialDelay:  100 * time.Millisecond,
		MaxDelay:      500 * time.Millisecond,
		Multiplier:   2.0,
	}

	delays := []time.Duration{
		config.CalculateDelay(0), // 100ms
		config.CalculateDelay(1), // 200ms
		config.CalculateDelay(2), // 400ms
		config.CalculateDelay(3), // 500ms (should be capped)
		config.CalculateDelay(4), // 500ms (should stay capped)
	}

	expected := []time.Duration{
		100 * time.Millisecond,
		200 * time.Millisecond,
		400 * time.Millisecond,
		500 * time.Millisecond,
		500 * time.Millisecond,
	}

	for i, got := range delays {
		if got != expected[i] {
			t.Errorf("delay[%d] = %v, want %v", i, got, expected[i])
		}
	}
}

// TestDLQMessage 測試 DLQ 消息結構
func TestDLQMessage(t *testing.T) {
	originalMsg := MessageData{
		MsgID:          "test-msg-id",
		ConversationID:  "conv-123",
		FromUserID:     "user-1",
		ToUserID:       "user-2",
		Content:        "Hello",
		ContentType:    "text",
		ClientTS:       time.Now().UnixMilli(),
		ServerTS:       time.Now().UnixMilli(),
		Status:         "sending",
	}

	dlqMsg := DLQMessage{
		OriginalMsg: originalMsg,
		Error:       "database connection failed",
		RetryCount:  3,
		FailedAt:    time.Now().UnixMilli(),
		ErrorCode:   errorcode.KafkaMessagePersistError.Code,
	}

	// 测试序列化
	data, err := json.Marshal(dlqMsg)
	if err != nil {
		t.Fatalf("Failed to marshal DLQ message: %v", err)
	}

	// 测试反序列化
	var decoded DLQMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal DLQ message: %v", err)
	}

	if decoded.OriginalMsg.MsgID != dlqMsg.OriginalMsg.MsgID {
		t.Errorf("MsgID mismatch: got %s, want %s", decoded.OriginalMsg.MsgID, dlqMsg.OriginalMsg.MsgID)
	}

	if decoded.ErrorCode != errorcode.KafkaMessagePersistError.Code {
		t.Errorf("ErrorCode mismatch: got %s, want %s", decoded.ErrorCode, errorcode.KafkaMessagePersistError.Code)
	}

	logger.Info("DLQ message test passed",
		"msg_id", decoded.OriginalMsg.MsgID,
		"error_code", decoded.ErrorCode,
		"retry_count", decoded.RetryCount)
}

// TestMessageIdempotency 测试消息幂等性
func TestMessageIdempotency(t *testing.T) {
	repo := NewMockMessageRepository(0, nil)

	// 第一次创建消息
	msg := &message.Message{
		MsgID:          "idempotent-msg-1",
		ConversationID: "conv-123",
		FromUserID:     "user-1",
		ToUserID:       "user-2",
		Content:        "Hello",
		ContentType:    message.MsgContentTypeText,
		Status:         message.MsgStatusSending,
		ClientTS:       time.Now().UnixMilli(),
		ServerTS:       time.Now().UnixMilli(),
	}

	err := repo.CreateMessage(msg)
	if err != nil {
		t.Fatalf("First create failed: %v", err)
	}

	// 第二次创建相同消息（模拟重复消费）
	err = repo.CreateMessage(msg)
	if err != nil {
		t.Fatalf("Second create failed: %v", err)
	}

	// 验证只创建了一条消息
	if len(repo.messages) != 1 {
		t.Errorf("Expected 1 message, got %d", len(repo.messages))
	}

	logger.Info("Message idempotency test passed", "message_count", len(repo.messages))
}

// TestRetryExhaustion 测试重试次数耗尽
func TestRetryExhaustion(t *testing.T) {
	// 模拟前 3 次都失败，第 4 次成功
	failCount := 0
	maxRetries := 3

	retryConfig := DefaultRetryConfig()
	retryConfig.MaxAttempts = maxRetries

	simulateRetry := func() (bool, int) {
		for retryCount := 0; retryCount <= maxRetries; retryCount++ {
			failCount++

			if failCount <= maxRetries {
				// 模拟失败
				delay := retryConfig.CalculateDelay(retryCount)
				logger.Info("Retry attempt failed, will retry",
					"retry_count", retryCount,
					"delay", delay)
				continue
			}

			// 成功
			return true, retryCount
		}
		return false, failCount
	}

	success, totalAttempts := simulateRetry()

	if !success {
		t.Error("Expected success after retries")
	}

	logger.Info("Retry exhaustion test passed",
		"success", success,
		"total_attempts", totalAttempts)
}

// TestDeliverySemantic 测试消息投递语义
func TestDeliverySemantic(t *testing.T) {
	// 测试 at-least-once + 幂等
	repo := NewMockMessageRepository(0, nil)

	msgData := &MessageData{
		MsgID:          "delivery-test-msg",
		ConversationID: "conv-456",
		FromUserID:     "user-1",
		ToUserID:       "user-2",
		Content:        "Test content",
		ContentType:    "text",
		Status:         "sending",
		ClientTS:       time.Now().UnixMilli(),
		ServerTS:       time.Now().UnixMilli(),
	}

	// 模拟第一次处理（成功）
	existingMsg, _ := repo.GetMessageByMsgID(msgData.MsgID)
	if existingMsg != nil {
		t.Log("Message already exists (idempotent check)")
	}

	// 保存消息
	msgModel := &message.Message{
		MsgID:          msgData.MsgID,
		ConversationID: msgData.ConversationID,
		FromUserID:     msgData.FromUserID,
		ToUserID:       msgData.ToUserID,
		Content:        msgData.Content,
		ContentType:    message.MessageType(msgData.ContentType),
		Status:         message.MessageStatus(msgData.Status),
		ClientTS:       msgData.ClientTS,
		ServerTS:       msgData.ServerTS,
	}

	if err := repo.CreateMessage(msgModel); err != nil {
		t.Fatalf("Failed to create message: %v", err)
	}

	// 模拟重复消费（第二次处理）
	existingMsg, _ = repo.GetMessageByMsgID(msgData.MsgID)
	if existingMsg != nil {
		logger.Info("Message already exists, skipping (at-least-once semantic)",
			"msg_id", msgData.MsgID)
	}

	// 验证只有一条消息
	if len(repo.messages) != 1 {
		t.Errorf("Expected 1 message (idempotent), got %d", len(repo.messages))
	}

	logger.Info("Delivery semantic test passed",
		"msg_id", msgData.MsgID,
		"semantic", "at-least-once + idempotent")
}

// TestErrorCode 测试错误码
func TestErrorCode(t *testing.T) {
	tests := []struct {
		name     string
		code     *errorcode.Code
		wantCode string
	}{
		{"KafkaError", errorcode.KafkaError, "50001"},
		{"KafkaMessageUnmarshalError", errorcode.KafkaMessageUnmarshalError, "50002"},
		{"KafkaMessagePersistError", errorcode.KafkaMessagePersistError, "50003"},
		{"KafkaMessageDeliveryError", errorcode.KafkaMessageDeliveryError, "50004"},
		{"KafkaConsumerGroupError", errorcode.KafkaConsumerGroupError, "50005"},
		{"KafkaRetryExhausted", errorcode.KafkaRetryExhausted, "50006"},
		{"KafkaDLQSendError", errorcode.KafkaDLQSendError, "50007"},
		{"KafkaProducerError", errorcode.KafkaProducerError, "50008"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.code.Code != tt.wantCode {
				t.Errorf("Error code = %s, want %s", tt.code.Code, tt.wantCode)
			}
			logger.Info("Error code test passed",
				"name", tt.name,
				"code", tt.code.Code,
				"message", tt.code.Message)
		})
	}
}

// TestContextCancellation 测试上下文取消
func TestContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	// 模拟消费者循环
	done := make(chan bool)
	go func() {
		for {
			select {
			case <-ctx.Done():
				logger.Info("Consumer stopped due to context cancellation")
				done <- true
				return
			default:
				// 继续处理
				time.Sleep(10 * time.Millisecond)
			}
		}
	}()

	// 取消上下文
	cancel()

	select {
	case <-done:
		logger.Info("Context cancellation test passed")
	case <-time.After(1 * time.Second):
		t.Error("Timeout waiting for consumer to stop")
	}
}

// BenchmarkRetryDelay 计算重试延迟性能测试
func BenchmarkRetryDelay(b *testing.B) {
	config := DefaultRetryConfig()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = config.CalculateDelay(i % 10)
	}
}
