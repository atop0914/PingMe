package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"PingMe/internal/gateway"
	"PingMe/internal/logger"
	"PingMe/internal/model/message"
	msgrepo "PingMe/internal/repository/message"
	"PingMe/internal/repository/user"

	"github.com/google/uuid"
)

// MessageService 消息服务
type MessageService struct {
	msgRepo *msgrepo.Repository
	userRepo *user.Repository
	hub     *gateway.Hub
}

// NewMessageService 创建消息服务
func NewMessageService(msgRepo *msgrepo.Repository, userRepo *user.Repository, hub *gateway.Hub) *MessageService {
	return &MessageService{
		msgRepo:  msgRepo,
		userRepo: userRepo,
		hub:      hub,
	}
}

// SendMessage 发送消息
func (s *MessageService) SendMessage(ctx context.Context, fromUserID string, req *message.SendMessageRequest) (*message.SendMessageResponse, error) {
	// 获取或创建会话
	conv, err := s.msgRepo.GetOrCreatePrivateConversation(fromUserID, req.ToUserID)
	if err != nil {
		logger.Error("Failed to get or create conversation",
			"from_user_id", fromUserID,
			"to_user_id", req.ToUserID,
			"error", err)
		return nil, fmt.Errorf("failed to get conversation: %w", err)
	}

	// 创建消息
	msg := &message.Message{
		MsgID:          uuid.New().String(),
		ConversationID: conv.ConversationID,
		FromUserID:     fromUserID,
		ToUserID:       req.ToUserID,
		Content:        req.Content,
		ContentType:    req.ContentType,
		Status:         message.MsgStatusSending,
		ClientTS:       req.ClientTS,
		ServerTS:       time.Now().UnixMilli(),
	}

	// 保存消息到数据库
	if err := s.msgRepo.CreateMessage(msg); err != nil {
		logger.Error("Failed to create message",
			"error", err)
		return nil, fmt.Errorf("failed to create message: %w", err)
	}

	// 尝试在线实时投递
	delivered := s.deliverMessage(ctx, req.ToUserID, msg)

	// 更新消息状态
	if delivered {
		msg.Status = message.MsgStatusSent
	} else {
		// 离线消息，状态保持 sending，客户端会通过拉取补齐
	}
	s.msgRepo.UpdateMessageStatus(msg.MsgID, msg.Status)

	// 更新会话最后活跃时间
	s.msgRepo.UpdateConversationTime(conv.ConversationID)

	logger.Info("Message sent",
		"msg_id", msg.MsgID,
		"from_user_id", fromUserID,
		"to_user_id", req.ToUserID,
		"delivered", delivered)

	return &message.SendMessageResponse{
		MsgID:          msg.MsgID,
		ConversationID: conv.ConversationID,
		Status:         msg.Status,
		ServerTS:       msg.ServerTS,
	}, nil
}

// deliverMessage 投递消息
// 返回 true 表示在线投递成功，false 表示离线
func (s *MessageService) deliverMessage(ctx context.Context, toUserID string, msg *message.Message) bool {
	// 构建消息体
	msgData := s.buildPushMessage(msg)

	// 尝试本地投递
	h := s.hub
	conns := h.GetUserConnections(toUserID)

	if conns != nil && len(conns) > 0 {
		// 用户在线，本地投递
		for _, conn := range conns {
			select {
			case conn.Send <- msgData:
			default:
				logger.Warn("Connection send buffer full",
					"conn_id", conn.ID,
					"user_id", toUserID)
			}
		}
		logger.Debug("Message delivered locally",
			"to_user_id", toUserID)
		return true
	}

	// 本地无连接，检查 Redis
	if h.RedisClient != nil {
		presence, err := h.RedisClient.GetUserPresence(ctx, toUserID)
		if err != nil {
			logger.Error("Failed to get user presence",
				"to_user_id", toUserID,
				"error", err)
			return false
		}

		if presence != nil {
			// 用户在其他实例在线
			// TODO: 跨实例投递（Kafka 或 RPC）
			logger.Info("User online on other instance, message stored for pull",
				"to_user_id", toUserID,
				"instance_id", presence.InstanceID)
			return false
		}
	}

	// 用户离线
	logger.Debug("User offline, message stored",
		"to_user_id", toUserID)
	return false
}

// buildPushMessage 构建推送消息体
func (s *MessageService) buildPushMessage(msg *message.Message) []byte {
	pushMsg := map[string]interface{}{
		"type": "message",
		"payload": map[string]interface{}{
			"msg_id":          msg.MsgID,
			"conversation_id": msg.ConversationID,
			"from_user_id":    msg.FromUserID,
			"content":         msg.Content,
			"content_type":    msg.ContentType,
			"status":          msg.Status,
			"server_ts":       msg.ServerTS,
			"client_ts":       msg.ClientTS,
		},
	}
	data, _ := json.Marshal(pushMsg)
	return data
}

// GetHistory 获取历史消息
func (s *MessageService) GetHistory(ctx context.Context, userID, conversationID string, limit int, beforeMsgID string) (*message.GetHistoryResponse, error) {
	// 验证用户是否为会话成员
	isMember, err := s.msgRepo.IsConversationMember(conversationID, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to check membership: %w", err)
	}
	if !isMember {
		return nil, fmt.Errorf("not a member of this conversation")
	}

	// 获取消息
	messages, err := s.msgRepo.GetMessagesByConversation(conversationID, limit, beforeMsgID)
	if err != nil {
		return nil, fmt.Errorf("failed to get messages: %w", err)
	}

	// 反转顺序（按时间正序）
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}

	hasMore := len(messages) > limit
	if hasMore {
		messages = messages[:limit]
	}

	var nextCursor string
	if len(messages) > 0 && hasMore {
		lastMsg := messages[len(messages)-1]
		nextCursor = fmt.Sprintf("%d", lastMsg.ServerTS)
	}

	return &message.GetHistoryResponse{
		Messages:   messages,
		HasMore:    hasMore,
		NextCursor: nextCursor,
	}, nil
}

// GetConversations 获取用户的会话列表
func (s *MessageService) GetConversations(ctx context.Context, userID string, limit int, cursor string) (*message.GetConversationsResponse, error) {
	conversations, nextCursor, err := s.msgRepo.GetUserConversations(userID, limit, cursor)
	if err != nil {
		return nil, fmt.Errorf("failed to get conversations: %w", err)
	}

	result := make([]message.ConversationWithLastMessage, len(conversations))
	for i, conv := range conversations {
		lastMsg, _ := s.msgRepo.GetLastMessage(conv.ConversationID)
		unreadCount, _ := s.msgRepo.CountUnread(conv.ConversationID, userID)

		result[i] = message.ConversationWithLastMessage{
			Conversation: conv,
			LastMessage:  lastMsg,
			UnreadCount:  int(unreadCount),
		}
	}

	hasMore := nextCursor != ""

	return &message.GetConversationsResponse{
		Conversations: result,
		HasMore:       hasMore,
		NextCursor:    nextCursor,
	}, nil
}

// PullOfflineMessages 拉取离线消息
func (s *MessageService) PullOfflineMessages(ctx context.Context, userID string, lastTS int64, limit int) ([]message.Message, error) {
	// 获取用户所有会话
	conversations, _, err := s.msgRepo.GetUserConversations(userID, 100, "")
	if err != nil {
		return nil, err
	}

	var allMessages []message.Message
	for _, conv := range conversations {
		msgs, err := s.msgRepo.GetMessagesAfter(conv.ConversationID, lastTS, limit)
		if err != nil {
			logger.Warn("Failed to get offline messages for conversation",
				"conversation_id", conv.ConversationID,
				"error", err)
			continue
		}
		allMessages = append(allMessages, msgs...)
	}

	// 按时间排序
	if len(allMessages) > 0 {
		// 消息已经是按会话内时间正序的，整体按 server_ts 排序
		// 这里简单处理，实际可能需要更复杂的合并排序
	}

	return allMessages, nil
}

// GetConversation 获取会话详情
func (s *MessageService) GetConversation(conversationID string) (*message.Conversation, error) {
	return s.msgRepo.GetConversationByID(conversationID)
}

// IsMember 检查用户是否为会话成员
func (s *MessageService) IsMember(conversationID, userID string) (bool, error) {
	return s.msgRepo.IsConversationMember(conversationID, userID)
}
