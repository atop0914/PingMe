package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"PingMe/internal/config"
	"PingMe/internal/logger"
	"PingMe/internal/model/user"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // 生产环境应该检查 Origin
	},
}

// MessageType 定义消息类型
type MessageType string

const (
	MsgTypePing    MessageType = "ping"
	MsgTypePong    MessageType = "pong"
	MsgTypeText    MessageType = "text"
	MsgTypeError   MessageType = "error"
	MsgTypeAuth    MessageType = "auth"
	MsgTypeAuthOK  MessageType = "auth_ok"
	MsgTypeAuthFail MessageType = "auth_fail"
)

// BaseMessage 基础消息结构
type BaseMessage struct {
	Type    MessageType `json:"type"`
	Payload interface{} `json:"payload,omitempty"`
}

// TextMessage 文本消息
type TextMessage struct {
	Content   string `json:"content"`
	MsgID     string `json:"msg_id"`
	Timestamp int64  `json:"timestamp"`
}

// ErrorMessage 错误消息
type ErrorMessage struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Connection WebSocket 连接
type Connection struct {
	ID        string
	UserID    string
	Socket    *websocket.Conn
	Hub       *Hub
	Send      chan []byte
	IsAuth    atomic.Bool
	RateLimiter *RateLimiter
	CreatedAt time.Time
	LastActive time.Time
	mu        sync.Mutex
}

// Hub 管理所有连接
type Hub struct {
	Connections map[string]*Connection
	UserConns   map[string]map[string]*Connection // userID -> connID -> Connection
	Broadcast   chan []byte
	Register    chan *Connection
	Unregister  chan *Connection
	mu          sync.RWMutex
	Config      *config.Config
	wg          sync.WaitGroup
}

// RateLimiter 连接级别限流器
type RateLimiter struct {
	MaxMessagesPerSec int
	MaxMessageSize    int64
	MessageCount      int
	LastReset         time.Time
	mu                sync.Mutex
}

// RateLimitExceededError 限流错误
type RateLimitExceededError struct {
	Message string
}

func (e *RateLimitExceededError) Error() string {
	return e.Message
}

// JWT Claims
type JWTClaims struct {
	UserID   string `json:"user_id"`
	Username string `json:"username"`
	jwt.RegisteredClaims
}

// NewHub 创建新的 Hub
func NewHub(cfg *config.Config) *Hub {
	return &Hub{
		Connections: make(map[string]*Connection),
		UserConns:   make(map[string]map[string]*Connection),
		Broadcast:   make(chan []byte, 256),
		Register:    make(chan *Connection),
		Unregister:  make(chan *Connection),
		Config:      cfg,
	}
}

// Run 启动 Hub
func (h *Hub) Run(ctx context.Context) {
	h.wg.Add(1)
	defer h.wg.Done()

	for {
		select {
		case <-ctx.Done():
			// 清理所有连接
			h.mu.Lock()
			for _, conn := range h.Connections {
				close(conn.Send)
				if conn.Socket != nil {
					conn.Socket.Close()
				}
			}
			h.mu.Unlock()
			return

		case conn := <-h.Register:
			h.mu.Lock()
			h.Connections[conn.ID] = conn

			if _, ok := h.UserConns[conn.UserID]; !ok {
				h.UserConns[conn.UserID] = make(map[string]*Connection)
			}
			h.UserConns[conn.UserID][conn.ID] = conn
			h.mu.Unlock()

			logger.Info("New connection registered",
				"conn_id", conn.ID,
				"user_id", conn.UserID)

		case conn := <-h.Unregister:
			h.mu.Lock()
			if _, ok := h.Connections[conn.ID]; ok {
				delete(h.Connections, conn.ID)
				if userConns, ok := h.UserConns[conn.UserID]; ok {
					delete(userConns, conn.ID)
					if len(userConns) == 0 {
						delete(h.UserConns, conn.UserID)
					}
				}
				close(conn.Send)
			}
			h.mu.Unlock()

			logger.Info("Connection unregistered",
				"conn_id", conn.ID,
				"user_id", conn.UserID)
		}
	}
}

// NewConnection 创建新连接
func NewConnection(id string, socket *websocket.Conn, hub *Hub, userID string) *Connection {
	return &Connection{
		ID:          id,
		UserID:      userID,
		Socket:      socket,
		Hub:         hub,
		Send:        make(chan []byte, 256),
		IsAuth:      atomic.Bool{},
		RateLimiter: NewRateLimiter(10, hub.Config.WebSocket.MaxMessageSize), // 10 messages/sec
		CreatedAt:   time.Now(),
		LastActive:  time.Now(),
	}
}

// NewRateLimiter 创建限流器
func NewRateLimiter(maxMessagesPerSec int, maxMessageSize int64) *RateLimiter {
	return &RateLimiter{
		MaxMessagesPerSec: maxMessagesPerSec,
		MaxMessageSize:    maxMessageSize,
		MessageCount:      0,
		LastReset:         time.Now(),
	}
}

// Allow 检查是否允许发送消息
func (rl *RateLimiter) Allow(msgSize int64) error {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	if now.Sub(rl.LastReset) >= time.Second {
		rl.MessageCount = 0
		rl.LastReset = now
	}

	if rl.MessageCount >= rl.MaxMessagesPerSec {
		return &RateLimitExceededError{
			Message: fmt.Sprintf("Rate limit exceeded: max %d messages per second", rl.MaxMessagesPerSec),
		}
	}

	if msgSize > rl.MaxMessageSize {
		return fmt.Errorf("message size %d exceeds maximum allowed size %d", msgSize, rl.MaxMessageSize)
	}

	rl.MessageCount++
	return nil
}

// WritePump 处理写操作
func (c *Connection) WritePump() {
	ticker := time.NewTicker(time.Duration(c.Hub.Config.WebSocket.PingInterval) * time.Second)
	defer func() {
		ticker.Stop()
		c.Socket.Close()
	}()

	for {
		select {
		case message, ok := <-c.Send:
			c.Socket.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				c.Socket.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.Socket.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)

			if err := w.Close(); err != nil {
				return
			}

		case <-ticker.C:
			c.Socket.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.Socket.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// ReadPump 处理读操作
func (c *Connection) ReadPump() {
	defer func() {
		c.Hub.Unregister <- c
		c.Socket.Close()
	}()

	c.Socket.SetReadLimit(c.Hub.Config.WebSocket.MaxMessageSize)
	c.Socket.SetReadDeadline(time.Now().Add(time.Duration(c.Hub.Config.WebSocket.PongTimeout) * time.Second))
	c.Socket.SetPongHandler(func(string) error {
		c.Socket.SetReadDeadline(time.Now().Add(time.Duration(c.Hub.Config.WebSocket.PongTimeout) * time.Second))
		c.LastActive = time.Now()
		return nil
	})

	for {
		_, message, err := c.Socket.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				logger.Warn("WebSocket unexpected close error",
					"conn_id", c.ID,
					"error", err)
			}
			break
		}

		c.LastActive = time.Now()

		// 限流检查
		if err := c.RateLimiter.Allow(int64(len(message))); err != nil {
			errorMsg := BaseMessage{
				Type: MsgTypeError,
				Payload: ErrorMessage{
					Code:    429,
					Message: err.Error(),
				},
			}
			errorData, _ := json.Marshal(errorMsg)
			c.Send <- errorData
			continue
		}

		// 处理消息
		c.handleMessage(message)
	}
}

// handleMessage 处理收到的消息
func (c *Connection) handleMessage(data []byte) {
	var msg BaseMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		logger.Warn("Invalid message format",
			"conn_id", c.ID,
			"error", err)
		return
	}

	switch msg.Type {
	case MsgTypeAuth:
		c.handleAuth(data)
	case MsgTypePing:
		c.handlePing()
	case MsgTypeText:
		c.handleText(data)
	default:
		logger.Warn("Unknown message type",
			"conn_id", c.ID,
			"type", string(msg.Type))
	}
}

// handleAuth 处理认证消息
func (c *Connection) handleAuth(data []byte) {
	var authMsg struct {
		Type    MessageType `json:"type"`
		Payload struct {
			Token string `json:"token"`
		} `json:"payload"`
	}

	if err := json.Unmarshal(data, &authMsg); err != nil {
		c.sendAuthFail("Invalid auth message format")
		return
	}

	// 验证 JWT token
	claims, err := validateToken(authMsg.Payload.Token, c.Hub.Config.JWT.Secret)
	if err != nil {
		c.sendAuthFail("Invalid token: " + err.Error())
		return
	}

	c.mu.Lock()
	c.UserID = claims.UserID
	c.IsAuth.Store(true)
	c.mu.Unlock()

	// 重新注册到 Hub（带上 userID）
	c.Hub.Unregister <- c
	c.Hub.Register <- c

	// 发送认证成功
	authOK := BaseMessage{
		Type: MsgTypeAuthOK,
		Payload: map[string]interface{}{
			"user_id":  c.UserID,
			"username": claims.Username,
		},
	}
	authData, _ := json.Marshal(authOK)
	c.Send <- authData

	logger.Info("Client authenticated",
		"conn_id", c.ID,
		"user_id", c.UserID)
}

// sendAuthFail 发送认证失败消息
func (c *Connection) sendAuthFail(reason string) {
	authFail := BaseMessage{
		Type: MsgTypeAuthFail,
		Payload: ErrorMessage{
			Code:    401,
			Message: reason,
		},
	}
	authFailData, _ := json.Marshal(authFail)
	c.Send <- authFailData
}

// handlePing 处理心跳
func (c *Connection) handlePing() {
	pong := BaseMessage{
		Type: MsgTypePong,
		Payload: map[string]interface{}{
			"timestamp": time.Now().UnixMilli(),
		},
	}
	pongData, _ := json.Marshal(pong)
	c.Send <- pongData
}

// handleText 处理文本消息
func (c *Connection) handleText(data []byte) {
	if !c.IsAuth.Load() {
		errorMsg := BaseMessage{
			Type: MsgTypeError,
			Payload: ErrorMessage{
				Code:    401,
				Message: "Not authenticated",
			},
		}
		errorData, _ := json.Marshal(errorMsg)
		c.Send <- errorData
		return
	}

	var textMsg TextMessage
	if err := json.Unmarshal(data, &textMsg); err != nil {
		logger.Warn("Invalid text message format",
			"conn_id", c.ID,
			"error", err)
		return
	}

	logger.Info("Text message received",
		"conn_id", c.ID,
		"user_id", c.UserID,
		"content", textMsg.Content)

	// TODO: 后续 Day 4-5 实现消息投递到 Kafka
}

// validateToken 验证 JWT token
func validateToken(tokenString string, secret string) (*JWTClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &JWTClaims{}, func(token *jwt.Token) (interface{}, error) {
		return []byte(secret), nil
	})

	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(*JWTClaims); ok && token.Valid {
		return claims, nil
	}

	return nil, fmt.Errorf("invalid token claims")
}

// GetUserConnections 获取用户的所有连接
func (h *Hub) GetUserConnections(userID string) []*Connection {
	h.mu.RLock()
	defer h.mu.RUnlock()

	conns, ok := h.UserConns[userID]
	if !ok {
		return nil
	}

	result := make([]*Connection, 0, len(conns))
	for _, conn := range conns {
		result = append(result, conn)
	}
	return result
}

// SendToUser 发送消息给指定用户
func (h *Hub) SendToUser(userID string, message []byte) {
	h.mu.RLock()
	conns, ok := h.UserConns[userID]
	h.mu.RUnlock()

	if !ok {
		logger.Debug("User not connected",
			"user_id", userID)
		return
	}

	for _, conn := range conns {
		select {
		case conn.Send <- message:
		default:
			logger.Warn("User connection send buffer full",
				"user_id", userID,
				"conn_id", conn.ID)
		}
	}
}

// GetStats 获取 Hub 统计信息
func (h *Hub) GetStats() map[string]interface{} {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return map[string]interface{}{
		"total_connections": len(h.Connections),
		"total_users":       len(h.UserConns),
	}
}

// BroadcastToUsers 广播消息给多个用户
func (h *Hub) BroadcastToUsers(userIDs []string, message []byte) {
	for _, userID := range userIDs {
		h.SendToUser(userID, message)
	}
}

// HandleWebSocket 处理 WebSocket 连接请求
func HandleWebSocket(hub *Hub) gin.HandlerFunc {
	return func(c *gin.Context) {
		socket, err := upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			logger.Error("Failed to upgrade connection", "error", err)
			return
		}

		connID := fmt.Sprintf("%d", time.Now().UnixNano())
		conn := NewConnection(connID, socket, hub, "")

		hub.Register <- conn

		go conn.WritePump()
		go conn.ReadPump()
	}
}

// 创建用户 Profile 响应
func createUserProfile(u *user.User) map[string]interface{} {
	return map[string]interface{}{
		"user_id":  u.UserID,
		"username": u.Username,
		"nickname": u.Nickname,
	}
}
