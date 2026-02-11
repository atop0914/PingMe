package gateway

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"PingMe/internal/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func getTestConfig() *config.Config {
	return &config.Config{
		App: config.AppConfig{
			Host: "0.0.0.0",
			Port: 8080,
		},
		JWT: config.JWTConfig{
			Secret:     "test-secret-key",
			Expiration: 86400,
		},
		WebSocket: config.WebSocketConfig{
			PingInterval:  30,
			PongTimeout:   60,
			MaxMessageSize: 8192,
		},
	}
}

func TestNewHub(t *testing.T) {
	cfg := getTestConfig()
	hub := NewHub(cfg)

	assert.NotNil(t, hub)
	assert.NotNil(t, hub.Connections)
	assert.NotNil(t, hub.UserConns)
	assert.NotNil(t, hub.Broadcast)
	assert.NotNil(t, hub.Register)
	assert.NotNil(t, hub.Unregister)
	assert.Equal(t, cfg, hub.Config)
}

func TestNewConnection(t *testing.T) {
	cfg := getTestConfig()
	hub := NewHub(cfg)

	conn := NewConnection("test-conn-id", nil, hub, "test-user-id")

	assert.Equal(t, "test-conn-id", conn.ID)
	assert.Equal(t, "test-user-id", conn.UserID)
	assert.Nil(t, conn.Socket)
	assert.NotNil(t, conn.Send)
	assert.False(t, conn.IsAuth.Load())
	assert.NotNil(t, conn.RateLimiter)
	assert.False(t, conn.CreatedAt.IsZero())
	assert.False(t, conn.LastActive.IsZero())
}

func TestNewRateLimiter(t *testing.T) {
	rl := NewRateLimiter(10, 1024)

	assert.Equal(t, 10, rl.MaxMessagesPerSec)
	assert.Equal(t, int64(1024), rl.MaxMessageSize)
	assert.Equal(t, 0, rl.MessageCount)
}

func TestRateLimiter_Allow(t *testing.T) {
	rl := NewRateLimiter(2, 100) // 2 messages/sec, max 100 bytes

	// Allow should pass for first two messages
	err := rl.Allow(50)
	assert.NoError(t, err)
	assert.Equal(t, 1, rl.MessageCount)

	err = rl.Allow(50)
	assert.NoError(t, err)
	assert.Equal(t, 2, rl.MessageCount)

	// Third message should fail (rate limit exceeded)
	err = rl.Allow(50)
	assert.Error(t, err)
	assert.IsType(t, &RateLimitExceededError{}, err)
	assert.EqualError(t, err, "Rate limit exceeded: max 2 messages per second")

	// Create a new rate limiter for message size test
	rl2 := NewRateLimiter(10, 100) // max 100 bytes
	// Message size exceeded
	err = rl2.Allow(150)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds maximum allowed size")
}

func TestRateLimiter_Reset(t *testing.T) {
	rl := NewRateLimiter(5, 1000)
	rl.MessageCount = 5
	rl.LastReset = time.Now().Add(-2 * time.Second)

	// Should reset after 1 second
	err := rl.Allow(100)
	assert.NoError(t, err)
	assert.Equal(t, 1, rl.MessageCount)
}

func TestHub_GetUserConnections(t *testing.T) {
	cfg := getTestConfig()
	hub := NewHub(cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go hub.Run(ctx)

	// Register some connections
	conn1 := NewConnection("conn-1", nil, hub, "user-1")
	conn2 := NewConnection("conn-2", nil, hub, "user-1")
	conn3 := NewConnection("conn-3", nil, hub, "user-2")

	hub.Register <- conn1
	hub.Register <- conn2
	hub.Register <- conn3

	// Wait for registration
	time.Sleep(50 * time.Millisecond)

	conns := hub.GetUserConnections("user-1")
	require.Len(t, conns, 2)

	conns = hub.GetUserConnections("user-2")
	require.Len(t, conns, 1)

	conns = hub.GetUserConnections("user-3")
	assert.Nil(t, conns)
}

func TestHub_SendToUser(t *testing.T) {
	cfg := getTestConfig()
	hub := NewHub(cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go hub.Run(ctx)

	// Create a mock connection (simulate a real socket with a goroutine)
	conn := NewConnection("conn-1", nil, hub, "user-1")
	conn.IsAuth.Store(true)

	// Start a goroutine to receive messages from the connection
	go func() {
		for msg := range conn.Send {
			// Just consume the message
			_ = msg
		}
	}()

	hub.Register <- conn
	time.Sleep(50 * time.Millisecond)

	// Send message to user
	testMsg := []byte(`{"type":"test","payload":"hello"}`)
	hub.SendToUser("user-1", testMsg)

	// Give some time for the message to be sent
	time.Sleep(100 * time.Millisecond)

	// Verify no panic occurred and message was queued
	// (The message should be in the send buffer)
}

func TestHub_SendToUserNotConnected(t *testing.T) {
	cfg := getTestConfig()
	hub := NewHub(cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go hub.Run(ctx)

	// Should not panic when sending to non-existent user
	testMsg := []byte(`{"type":"test","payload":"hello"}`)
	hub.SendToUser("non-existent-user", testMsg)
	// Should complete without error
}

func TestJWTClaims(t *testing.T) {
	claims := &JWTClaims{
		UserID:   "user-123",
		Username: "testuser",
	}

	assert.Equal(t, "user-123", claims.UserID)
	assert.Equal(t, "testuser", claims.Username)
}

func TestMessageSerialization(t *testing.T) {
	// Test BaseMessage
	baseMsg := BaseMessage{
		Type:    MsgTypeText,
		Payload: "hello world",
	}

	data, err := json.Marshal(baseMsg)
	require.NoError(t, err)

	var decoded BaseMessage
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)
	assert.Equal(t, MsgTypeText, decoded.Type)

	// Test TextMessage
	textMsg := TextMessage{
		Content:   "Hello, World!",
		MsgID:     "msg-123",
		Timestamp: time.Now().UnixMilli(),
	}

	data, err = json.Marshal(textMsg)
	require.NoError(t, err)

	var decodedText TextMessage
	err = json.Unmarshal(data, &decodedText)
	require.NoError(t, err)
	assert.Equal(t, "Hello, World!", decodedText.Content)
	assert.Equal(t, "msg-123", decodedText.MsgID)

	// Test ErrorMessage
	errMsg := ErrorMessage{
		Code:    401,
		Message: "Unauthorized",
	}

	data, err = json.Marshal(errMsg)
	require.NoError(t, err)

	var decodedErr ErrorMessage
	err = json.Unmarshal(data, &decodedErr)
	require.NoError(t, err)
	assert.Equal(t, 401, decodedErr.Code)
	assert.Equal(t, "Unauthorized", decodedErr.Message)
}

func TestConnectionConcurrentAccess(t *testing.T) {
	cfg := getTestConfig()
	hub := NewHub(cfg)
	conn := NewConnection("test-id", nil, hub, "user-1")

	var wg sync.WaitGroup

	// Concurrent auth
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			conn.IsAuth.Store(true)
			_ = conn.IsAuth.Load()
		}()
	}

	// Concurrent user ID set
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			conn.mu.Lock()
			conn.UserID = "user-123"
			_ = conn.UserID
			conn.mu.Unlock()
		}()
	}

	wg.Wait()
	// If we get here without race detector catching anything, we're good
}

func TestRateLimitExceededError(t *testing.T) {
	err := &RateLimitExceededError{
		Message: "Rate limit exceeded",
	}

	assert.Equal(t, "Rate limit exceeded", err.Error())
	assert.IsType(t, &RateLimitExceededError{}, err)
}
