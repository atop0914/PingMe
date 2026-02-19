package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"PingMe/internal/gateway"
	"PingMe/internal/logger"
	"PingMe/internal/model/message"
	kafka2 "PingMe/internal/pkg/kafka"
	msgrepo "PingMe/internal/repository/message"
	"PingMe/internal/repository/user"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// MessageService 消息服务
type MessageService struct {
	msgRepo     *msgrepo.Repository
	userRepo    *user.Repository
	hub         *gateway.Hub
	kafkaProducer *kafka2.Producer
}

// NewMessageService 创建消息服务
func NewMessageService(msgRepo *msgrepo.Repository, userRepo *user.Repository, hub *gateway.Hub) *MessageService {
	return &MessageService{
		msgRepo:  msgRepo,
		userRepo: userRepo,
		hub:      hub,
	}
}

// NewMessageServiceWithKafka 创建带 Kafka 的消息服务
func NewMessageServiceWithKafka(msgRepo *msgrepo.Repository, userRepo *user.Repository, hub *gateway.Hub, producer *kafka2.Producer) *MessageService {
	return &MessageService{
		msgRepo:      msgRepo,
		userRepo:     userRepo,
		hub:          hub,
		kafkaProducer: producer,
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

	// 如果有 Kafka producer，发送到 Kafka
	if s.kafkaProducer != nil {
		kafkaMsg := &kafka2.MessageData{
			MsgID:          msg.MsgID,
			ConversationID: msg.ConversationID,
			FromUserID:     msg.FromUserID,
			ToUserID:       msg.ToUserID,
			Content:        msg.Content,
			ContentType:    string(msg.ContentType),
			ClientTS:       msg.ClientTS,
			ServerTS:       msg.ServerTS,
			Status:         string(msg.Status),
		}

		if err := s.kafkaProducer.SendMessage(kafkaMsg); err != nil {
			logger.Error("Failed to send message to Kafka",
				"msg_id", msg.MsgID,
				"error", err)
			// Kafka 发送失败，回退到直接落库
			if dbErr := s.msgRepo.CreateMessage(msg); dbErr != nil {
				logger.Error("Failed to create message",
					"error", dbErr)
				return nil, fmt.Errorf("failed to create message: %w", dbErr)
			}
		} else {
			logger.Info("Message sent to Kafka",
				"msg_id", msg.MsgID,
				"conversation_id", conv.ConversationID)
		}
	} else {
		// 没有 Kafka，直接落库
		if err := s.msgRepo.CreateMessage(msg); err != nil {
			logger.Error("Failed to create message",
				"error", err)
			return nil, fmt.Errorf("failed to create message: %w", err)
		}
	}

	// 更新会话最后活跃时间
	s.msgRepo.UpdateConversationTime(conv.ConversationID)

	logger.Info("Message sent",
		"msg_id", msg.MsgID,
		"from_user_id", fromUserID,
		"to_user_id", req.ToUserID)

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

// PullOfflineMessagesV2 使用游标拉取离线消息（改进版）
func (s *MessageService) PullOfflineMessagesV2(ctx context.Context, userID string, cursorStr string, limit int, conversationID string) (*message.PullOfflineMessagesResponse, error) {
	// 解码游标
	cursorTS, cursorID, err := message.DecodeCursor(cursorStr)
	if err != nil {
		logger.Warn("Invalid cursor format, using default",
			"cursor", cursorStr,
			"error", err)
		cursorTS, cursorID = 0, 0
	}

	// 如果指定了会话，只拉取该会话的消息
	if conversationID != "" {
		messages, err := s.msgRepo.GetMessagesByCursor(conversationID, cursorTS, cursorID, limit, true)
		if err != nil {
			return nil, fmt.Errorf("failed to get messages: %w", err)
		}

		// 构建响应
		hasMore := len(messages) > limit
		if hasMore {
			messages = messages[:limit]
		}

		var nextCursor string
		if len(messages) > 0 && hasMore {
			lastMsg := messages[len(messages)-1]
			nextCursor = message.EncodeCursor(lastMsg.ServerTS, lastMsg.ID)
		}

		return &message.PullOfflineMessagesResponse{
			Messages:   messages,
			HasMore:    hasMore,
			NextCursor: nextCursor,
		}, nil
	}

	// 拉取所有会话的离线消息
	messages, err := s.msgRepo.GetMessagesWithCursor(userID, cursorTS, cursorID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get offline messages: %w", err)
	}

	hasMore := len(messages) > limit
	if hasMore {
		messages = messages[:limit]
	}

	var nextCursor string
	if len(messages) > 0 && hasMore {
		lastMsg := messages[len(messages)-1]
		nextCursor = message.EncodeCursor(lastMsg.ServerTS, lastMsg.ID)
	}

	// 获取各会话未读数
	unreadCounts, err := s.msgRepo.GetConversationUnreadCounts(userID)
	if err != nil {
		logger.Warn("Failed to get unread counts",
			"error", err)
	}

	// 构建会话未读信息
	convUnreadList := make([]message.ConversationUnreadCount, 0)
	convSet := make(map[string]bool)
	for _, msg := range messages {
		if !convSet[msg.ConversationID] {
			convSet[msg.ConversationID] = true
			lastMsg, _ := s.msgRepo.GetLastMessage(msg.ConversationID)
			convUnreadList = append(convUnreadList, message.ConversationUnreadCount{
				ConversationID: msg.ConversationID,
				UnreadCount:    int(unreadCounts[msg.ConversationID]),
				LastMessage:    lastMsg,
			})
		}
	}

	return &message.PullOfflineMessagesResponse{
		Messages:      messages,
		HasMore:       hasMore,
		NextCursor:    nextCursor,
		Conversations: convUnreadList,
	}, nil
}

// MarkAsRead 标记消息已读
func (s *MessageService) MarkAsRead(ctx context.Context, userID string, req *message.MarkReadRequest) error {
	// 验证用户是否为会话成员
	isMember, err := s.msgRepo.IsConversationMember(req.ConversationID, userID)
	if err != nil {
		return fmt.Errorf("failed to check membership: %w", err)
	}
	if !isMember {
		return fmt.Errorf("not a member of this conversation")
	}

	// 获取消息信息
	msg, err := s.msgRepo.GetMessageByMsgID(req.MsgID)
	if err != nil {
		return fmt.Errorf("message not found: %w", err)
	}

	// 更新或创建用户游标
	cursor := &message.UserCursor{
		UserID:         userID,
		ConversationID: req.ConversationID,
		LastReadMsgID:  req.MsgID,
		LastReadTS:     msg.ServerTS,
		UpdatedAt:      time.Now(),
	}

	if err := s.msgRepo.UpsertUserCursor(cursor); err != nil {
		return fmt.Errorf("failed to update cursor: %w", err)
	}

	// 更新消息状态为已读
	if err := s.msgRepo.UpdateMessageStatus(req.MsgID, message.MsgStatusRead); err != nil {
		logger.Warn("Failed to update message status",
			"msg_id", req.MsgID,
			"error", err)
	}

	logger.Info("Marked messages as read",
		"user_id", userID,
		"conversation_id", req.ConversationID,
		"msg_id", req.MsgID,
		"server_ts", msg.ServerTS)

	return nil
}

// GetUnreadCount 获取指定会话的未读数
func (s *MessageService) GetUnreadCount(userID, conversationID string) (int64, error) {
	cursor, err := s.msgRepo.GetUserCursor(userID, conversationID)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			// 没有游标，说明从未已读该会话，返回所有消息数
			return s.msgRepo.CountUnread(conversationID, userID)
		}
		return 0, err
	}

	return s.msgRepo.GetUnreadCountByCursor(userID, conversationID, cursor.LastReadTS)
}

// ========== ACK 机制相关方法 ==========

// ProcessACK 处理客户端 ACK（幂等）
func (s *MessageService) ProcessACK(ctx context.Context, userID string, ack *message.ClientACK) (*message.ClientACKResponse, error) {
	// 1. 验证消息是否存在
	msg, err := s.msgRepo.GetMessageByMsgID(ack.MsgID)
	if err != nil {
		return nil, fmt.Errorf("message not found: %w", err)
	}

	// 2. 验证用户是否为会话成员
	isMember, err := s.msgRepo.IsConversationMember(ack.ConversationID, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to check membership: %w", err)
	}
	if !isMember {
		return nil, fmt.Errorf("not a member of this conversation")
	}

	// 3. 幂等处理：检查是否已存在相同 ACK
	serverTS := time.Now().UnixMilli()
	existingACK, err := s.msgRepo.GetACKByMsgIDAndUser(ack.MsgID, userID, ack.ACKType)
	if err == nil && existingACK != nil {
		// 重复 ACK，返回成功但不重复写入
		logger.Debug("Duplicate ACK received",
			"msg_id", ack.MsgID,
			"user_id", userID,
			"ack_type", ack.ACKType)
		
		return &message.ClientACKResponse{
			MsgID:    ack.MsgID,
			ACKType:  ack.ACKType,
			ServerTS: existingACK.ACKTS,
			Duplicate: true,
		}, nil
	}

	// 4. 创建 ACK 记录（幂等）
	ackRecord := &message.MessageACK{
		MsgID:          ack.MsgID,
		UserID:         userID,
		ConversationID: ack.ConversationID,
		ACKType:        ack.ACKType,
		ACKTS:          serverTS,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	if err := s.msgRepo.CreateOrUpdateACK(ackRecord); err != nil {
		return nil, fmt.Errorf("failed to save ACK: %w", err)
	}

	// 5. 更新消息状态
	if ack.ACKType == message.ACKTypeDelivered {
		// 更新为已送达
		if err := s.msgRepo.UpdateMessageStatus(ack.MsgID, message.MsgStatusDelivered); err != nil {
			logger.Warn("Failed to update message status to delivered",
				"msg_id", ack.MsgID,
				"error", err)
		}
	} else if ack.ACKType == message.ACKTypeRead {
		// 更新为已读
		if err := s.msgRepo.UpdateMessageStatus(ack.MsgID, message.MsgStatusRead); err != nil {
			logger.Warn("Failed to update message status to read",
				"msg_id", ack.MsgID,
				"error", err)
		}
		
		// 同时更新用户游标
		cursor := &message.UserCursor{
			UserID:         userID,
			ConversationID: ack.ConversationID,
			LastReadMsgID:  ack.MsgID,
			LastReadTS:     msg.ServerTS,
			UpdatedAt:      time.Now(),
		}
		if err := s.msgRepo.UpsertUserCursor(cursor); err != nil {
			logger.Warn("Failed to update user cursor",
				"user_id", userID,
				"error", err)
		}
	}

	// 6. 通知消息发送方（可选：通过 WebSocket 或其他方式）
	// 这里可以通知消息发送方消息已送达/已读

	logger.Info("ACK processed",
		"msg_id", ack.MsgID,
		"user_id", userID,
		"conversation_id", ack.ConversationID,
		"ack_type", ack.ACKType)

	return &message.ClientACKResponse{
		MsgID:     ack.MsgID,
		ACKType:   ack.ACKType,
		ServerTS:  serverTS,
		Duplicate: false,
	}, nil
}

// SyncACKs 同步 ACK 状态（用于 ACK 丢失补偿）
func (s *MessageService) SyncACKs(ctx context.Context, userID string, req *message.ACKSyncRequest) (*message.ACKSyncResponse, error) {
	// 验证用户是否为会话成员
	isMember, err := s.msgRepo.IsConversationMember(req.ConversationID, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to check membership: %w", err)
	}
	if !isMember {
		return nil, fmt.Errorf("not a member of this conversation")
	}

	response := &message.ACKSyncResponse{
		DeliveredACKs: []message.MessageACK{},
		ReadACKs:      []message.MessageACK{},
		MissedCount:   0,
	}

	// 如果未指定 ACK 类型，默认查询所有
	if len(req.ACKTypes) == 0 {
		req.ACKTypes = []message.ACKType{message.ACKTypeDelivered, message.ACKTypeRead}
	}

	// 遍历需要同步的 ACK 类型
	for _, ackType := range req.ACKTypes {
		// 获取服务端在该时间点之后的 ACK
		acks, err := s.msgRepo.GetACKsByConversation(userID, req.ConversationID, req.LastACKTS, ackType)
		if err != nil {
			logger.Warn("Failed to get ACKs for sync",
				"conversation_id", req.ConversationID,
				"ack_type", ackType,
				"error", err)
			continue
		}

		// 过滤掉客户端已知的 ACK（通过 lastAckMsgID）
		var missedACKs []message.MessageACK
		for _, ack := range acks {
			if ack.MsgID != req.LastACKMsgID {
				missedACKs = append(missedACKs, ack)
			}
		}

		response.MissedCount += len(missedACKs)

		if ackType == message.ACKTypeDelivered {
			response.DeliveredACKs = missedACKs
		} else if ackType == message.ACKTypeRead {
			response.ReadACKs = missedACKs
		}
	}

	logger.Info("ACK sync completed",
		"user_id", userID,
		"conversation_id", req.ConversationID,
		"missed_count", response.MissedCount)

	return response, nil
}

// CleanupOldACKs 清理过期的 ACK（回执表膨胀处理）
func (s *MessageService) CleanupOldACKs(retentionDays int) (int64, error) {
	// 默认保留 7 天
	if retentionDays <= 0 {
		retentionDays = 7
	}

	deletedCount, err := s.msgRepo.DeleteOldACKs(retentionDays)
	if err != nil {
		logger.Error("Failed to cleanup old ACKs",
			"error", err)
		return 0, err
	}

	logger.Info("Old ACKs cleaned up",
		"deleted_count", deletedCount,
		"retention_days", retentionDays)

	return deletedCount, nil
}

// GetACKStats 获取 ACK 统计信息
func (s *MessageService) GetACKStats() (map[string]int64, error) {
	stats := make(map[string]int64)

	// 总 ACK 数量
	total, err := s.msgRepo.GetACKCount()
	if err != nil {
		return nil, err
	}
	stats["total"] = total

	// 按类型统计
	deliveredCount, err := s.msgRepo.GetACKCountByType(message.ACKTypeDelivered)
	if err != nil {
		return nil, err
	}
	stats["delivered"] = deliveredCount

	readCount, err := s.msgRepo.GetACKCountByType(message.ACKTypeRead)
	if err != nil {
		return nil, err
	}
	stats["read"] = readCount

	return stats, nil
}

// NotifyMessageDelivered 通知消息已送达（当消息投递成功时调用）
func (s *MessageService) NotifyMessageDelivered(msgID, conversationID, toUserID string) error {
	// 创建送达 ACK
	serverTS := time.Now().UnixMilli()
	ack := &message.MessageACK{
		MsgID:          msgID,
		UserID:         toUserID,
		ConversationID: conversationID,
		ACKType:        message.ACKTypeDelivered,
		ACKTS:          serverTS,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	// 幂等写入
	if err := s.msgRepo.CreateOrUpdateACK(ack); err != nil {
		return fmt.Errorf("failed to create delivered ACK: %w", err)
	}

	// 更新消息状态
	if err := s.msgRepo.UpdateMessageStatus(msgID, message.MsgStatusDelivered); err != nil {
		logger.Warn("Failed to update message status to delivered",
			"msg_id", msgID,
			"error", err)
	}

	logger.Info("Message delivered notification processed",
		"msg_id", msgID,
		"to_user_id", toUserID)

	return nil
}

// ========== Gateway 适配器方法 ==========
// 用于处理来自 Gateway 的 ACK 请求（类型转换）

// ProcessGatewayACK 处理来自 Gateway 的 ACK 请求（适配器）
func (s *MessageService) ProcessGatewayACK(ctx context.Context, userID string, ack *gateway.ClientACKMessage) (*message.ClientACKResponse, error) {
	// 转换为 message.ClientACK
	msgACK := &message.ClientACK{
		MsgID:          ack.MsgID,
		ConversationID: ack.ConversationID,
		ACKType:        message.ACKType(ack.ACKType),
	}
	return s.ProcessACK(ctx, userID, msgACK)
}

// SyncGatewayACKs 处理来自 Gateway 的 ACK 同步请求（适配器）
func (s *MessageService) SyncGatewayACKs(ctx context.Context, userID string, req *gateway.ACKSyncRequestMessage) (*message.ACKSyncResponse, error) {
	// 转换为 message.ACKSyncRequest
	ackTypes := make([]message.ACKType, len(req.ACKTypes))
	for i, t := range req.ACKTypes {
		ackTypes[i] = message.ACKType(t)
	}

	syncReq := &message.ACKSyncRequest{
		ConversationID: req.ConversationID,
		LastACKMsgID:   req.LastACKMsgID,
		LastACKTS:      req.LastACKTS,
		ACKTypes:       ackTypes,
	}
	return s.SyncACKs(ctx, userID, syncReq)
}

// ========== 群组相关方法 ==========

// GroupService 群组服务
type GroupService struct {
	msgRepo      *msgrepo.Repository
	userRepo     *user.Repository
	hub          *gateway.Hub
	kafkaProducer *kafka2.Producer
}

// NewGroupService 创建群组服务
func NewGroupService(msgRepo *msgrepo.Repository, userRepo *user.Repository, hub *gateway.Hub, producer *kafka2.Producer) *GroupService {
	return &GroupService{
		msgRepo:      msgRepo,
		userRepo:     userRepo,
		hub:          hub,
		kafkaProducer: producer,
	}
}

// CreateGroup 创建群组
func (s *GroupService) CreateGroup(ctx context.Context, ownerUserID, groupName string, memberIDs []string) (*CreateGroupResponse, error) {
	// 生成群 ID
	groupID := "group:" + uuid.New().String()
	conversationID := "group:" + groupID

	// 构建成员列表（包含创建者）
	memberSet := make(map[string]bool)
	memberSet[ownerUserID] = true
	for _, id := range memberIDs {
		memberSet[id] = true
	}

	members := make([]message.ConversationMember, 0, len(memberSet))
	for userID := range memberSet {
		role := "member"
		if userID == ownerUserID {
			role = "owner"
		}
		members = append(members, message.ConversationMember{
			ConversationID: conversationID,
			UserID:         userID,
			Role:           role,
			JoinedAt:       time.Now(),
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
		})
	}

	// 事务创建
	err := s.msgRepo.CreateGroup(conversationID, groupID, groupName, ownerUserID, members)
	if err != nil {
		logger.Error("Failed to create group",
			"owner_user_id", ownerUserID,
			"group_name", groupName,
			"error", err)
		return nil, fmt.Errorf("failed to create group: %w", err)
	}

	logger.Info("Group created",
		"group_id", groupID,
		"owner_user_id", ownerUserID,
		"name", groupName,
		"member_count", len(members))

	// 返回成员ID列表
	memberIDList := make([]string, len(members))
	for i, m := range members {
		memberIDList[i] = m.UserID
	}

	return &CreateGroupResponse{
		GroupID:        groupID,
		Name:           groupName,
		ConversationID: conversationID,
		OwnerUserID:    ownerUserID,
		MemberIDs:      memberIDList,
		CreatedAt:      time.Now().UnixMilli(),
	}, nil
}

// JoinGroup 加群
func (s *GroupService) JoinGroup(ctx context.Context, userID, groupID string) error {
	// 获取群会话ID
	conversationID := "group:" + groupID

	// 检查群是否存在
	conv, err := s.msgRepo.GetGroupByGroupID(groupID)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return fmt.Errorf("group not found")
		}
		return fmt.Errorf("failed to get group: %w", err)
	}

	// 检查是否已经是成员
	isMember, err := s.msgRepo.IsConversationMember(conversationID, userID)
	if err != nil {
		return fmt.Errorf("failed to check membership: %w", err)
	}
	if isMember {
		return fmt.Errorf("already a group member")
	}

	// 添加成员
	member := &message.ConversationMember{
		ConversationID: conversationID,
		UserID:         userID,
		Role:           "member",
		JoinedAt:       time.Now(),
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	if err := s.msgRepo.AddGroupMember(member); err != nil {
		logger.Error("Failed to join group",
			"user_id", userID,
			"group_id", groupID,
			"error", err)
		return fmt.Errorf("failed to join group: %w", err)
	}

	logger.Info("User joined group",
		"user_id", userID,
		"group_id", groupID,
		"group_name", conv.Name)

	return nil
}

// LeaveGroup 退群
func (s *GroupService) LeaveGroup(ctx context.Context, userID, groupID string) error {
	conversationID := "group:" + groupID

	// 检查群是否存在
	conv, err := s.msgRepo.GetGroupByGroupID(groupID)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return fmt.Errorf("group not found")
		}
		return fmt.Errorf("failed to get group: %w", err)
	}

	// 群主不能退群（只能解散群）
	if conv.OwnerUserID == userID {
		return fmt.Errorf("owner cannot leave group, please dissolve the group instead")
	}

	// 检查是否是成员
	isMember, err := s.msgRepo.IsConversationMember(conversationID, userID)
	if err != nil {
		return fmt.Errorf("failed to check membership: %w", err)
	}
	if !isMember {
		return fmt.Errorf("not a group member")
	}

	// 移除成员
	if err := s.msgRepo.RemoveGroupMember(conversationID, userID); err != nil {
		logger.Error("Failed to leave group",
			"user_id", userID,
			"group_id", groupID,
			"error", err)
		return fmt.Errorf("failed to leave group: %w", err)
	}

	logger.Info("User left group",
		"user_id", userID,
		"group_id", groupID)

	return nil
}

// GetGroupMembers 获取群成员列表
func (s *GroupService) GetGroupMembers(groupID string) ([]message.ConversationMember, error) {
	conversationID := "group:" + groupID
	return s.msgRepo.GetConversationMembers(conversationID)
}

// GetGroupMembersWithStatus 获取群成员列表（带在线状态）
func (s *GroupService) GetGroupMembersWithStatus(ctx context.Context, groupID string, limit int) ([]GroupMemberWithStatus, error) {
	conversationID := "group:" + groupID

	// 获取所有成员
	members, err := s.msgRepo.GetConversationMembers(conversationID)
	if err != nil {
		return nil, fmt.Errorf("failed to get members: %w", err)
	}

	// 限制返回数量
	if len(members) > limit {
		members = members[:limit]
	}

	// 构建结果
	result := make([]GroupMemberWithStatus, len(members))
	for i, member := range members {
		online := false

		// 检查本地连接
		conns := s.hub.GetUserConnections(member.UserID)
		if conns != nil && len(conns) > 0 {
			online = true
		} else if s.hub.RedisClient != nil {
			// 检查 Redis 在线状态
			presence, _ := s.hub.RedisClient.GetUserPresence(ctx, member.UserID)
			online = presence != nil
		}

		result[i] = GroupMemberWithStatus{
			UserID:   member.UserID,
			Role:     member.Role,
			JoinedAt: member.JoinedAt.UnixMilli(),
			Online:   online,
		}
	}

	return result, nil
}

// IsGroupMember 检查用户是否为群成员
func (s *GroupService) IsGroupMember(groupID, userID string) (bool, error) {
	conversationID := "group:" + groupID
	return s.msgRepo.IsConversationMember(conversationID, userID)
}

// GetUserGroups 获取用户加入的群组列表
func (s *GroupService) GetUserGroups(ctx context.Context, userID string, limit int, cursor string) (*message.GetConversationsResponse, error) {
	// 复用 GetConversations 方法，筛选群组类型
	convs, nextCursor, err := s.msgRepo.GetUserConversations(userID, limit, cursor)
	if err != nil {
		return nil, fmt.Errorf("failed to get conversations: %w", err)
	}

	// 过滤群组类型并转换
	var groupConvs []message.ConversationWithLastMessage
	for _, conv := range convs {
		if conv.Type == message.ConvTypeGroup {
			groupConvs = append(groupConvs, message.ConversationWithLastMessage{
				Conversation: conv,
			})
		}
	}

	return &message.GetConversationsResponse{
		Conversations: groupConvs,
		HasMore:       nextCursor != "",
		NextCursor:    nextCursor,
	}, nil
}

// GetGroupInfo 获取群组详情
func (s *GroupService) GetGroupInfo(groupID string) (*message.Conversation, error) {
	conversationID := "group:" + groupID
	return s.msgRepo.GetConversationByID(conversationID)
}

// SendGroupMessage 发送群消息
func (s *GroupService) SendGroupMessage(ctx context.Context, fromUserID, groupID, content string, contentType message.MessageType, clientTS int64) (*SendGroupMessageResponse, error) {
	conversationID := "group:" + groupID

	// 检查群是否存在
	conv, err := s.msgRepo.GetGroupByGroupID(groupID)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("group not found")
		}
		return nil, fmt.Errorf("failed to get group: %w", err)
	}

	// 创建消息
	msg := &message.Message{
		MsgID:          uuid.New().String(),
		ConversationID: conversationID,
		FromUserID:     fromUserID,
		GroupID:        groupID,
		Content:         content,
		ContentType:    contentType,
		Status:         message.MsgStatusSending,
		ClientTS:       clientTS,
		ServerTS:       time.Now().UnixMilli(),
	}

	// 如果有 Kafka producer，发送到 Kafka
	if s.kafkaProducer != nil {
		kafkaMsg := &kafka2.MessageData{
			MsgID:          msg.MsgID,
			ConversationID: msg.ConversationID,
			FromUserID:     msg.FromUserID,
			GroupID:        msg.GroupID,
			Content:        msg.Content,
			ContentType:    string(msg.ContentType),
			ClientTS:       msg.ClientTS,
			ServerTS:       msg.ServerTS,
			Status:         string(msg.Status),
			IsGroup:        true,
		}

		if err := s.kafkaProducer.SendMessage(kafkaMsg); err != nil {
			logger.Error("Failed to send group message to Kafka",
				"msg_id", msg.MsgID,
				"error", err)
			// Kafka 发送失败，回退到直接落库
			if dbErr := s.msgRepo.CreateMessage(msg); dbErr != nil {
				logger.Error("Failed to create group message",
					"error", dbErr)
				return nil, fmt.Errorf("failed to create message: %w", dbErr)
			}
		} else {
			logger.Info("Group message sent to Kafka",
				"msg_id", msg.MsgID,
				"conversation_id", conversationID,
				"group_id", groupID)
		}
	} else {
		// 没有 Kafka，直接落库
		if err := s.msgRepo.CreateMessage(msg); err != nil {
			logger.Error("Failed to create group message",
				"error", err)
			return nil, fmt.Errorf("failed to create message: %w", err)
		}
	}

	// 更新群最后活跃时间
	s.msgRepo.UpdateConversationTime(conversationID)

	// 异步广播消息给群成员（优化：避免阻塞）
	go s.broadcastGroupMessage(msg, conv)

	logger.Info("Group message sent",
		"msg_id", msg.MsgID,
		"from_user_id", fromUserID,
		"group_id", groupID,
		"group_name", conv.Name)

	return &SendGroupMessageResponse{
		MsgID:          msg.MsgID,
		ConversationID: conversationID,
		Status:         string(msg.Status),
		ServerTS:       msg.ServerTS,
	}, nil
}

// broadcastGroupMessage 广播群消息（异步）
// 优化：采用批量投递策略，避免逐成员同步调用
func (s *GroupService) broadcastGroupMessage(msg *message.Message, conv *message.Conversation) {
	// 获取群成员
	members, err := s.msgRepo.GetConversationMembers(msg.ConversationID)
	if err != nil {
		logger.Error("Failed to get group members for broadcast",
			"group_id", msg.GroupID,
			"error", err)
		return
	}

	// 构建消息体
	msgData := s.buildGroupPushMessage(msg)

	// 统计在线/离线成员
	var onlineMembers []string
	var offlineMembers []string

	for _, member := range members {
		// 跳过自己
		if member.UserID == msg.FromUserID {
			continue
		}

		// 检查是否在线
		conns := s.hub.GetUserConnections(member.UserID)
		if conns != nil && len(conns) > 0 {
			onlineMembers = append(onlineMembers, member.UserID)
		} else if s.hub.RedisClient != nil {
			// 检查 Redis
			presence, err := s.hub.RedisClient.GetUserPresence(context.Background(), member.UserID)
			if err == nil && presence != nil {
				onlineMembers = append(onlineMembers, member.UserID)
			} else {
				offlineMembers = append(offlineMembers, member.UserID)
			}
		} else {
			offlineMembers = append(offlineMembers, member.UserID)
		}
	}

	// 批量发送在线成员（优化：减少锁竞争）
	for _, userID := range onlineMembers {
		conns := s.hub.GetUserConnections(userID)
		if conns != nil {
			for _, conn := range conns {
				select {
				case conn.Send <- msgData:
				default:
					logger.Warn("Connection send buffer full during group broadcast",
						"conn_id", conn.ID,
						"user_id", userID)
				}
			}
		}
	}

	logger.Info("Group message broadcast completed",
		"msg_id", msg.MsgID,
		"group_id", msg.GroupID,
		"total_members", len(members),
		"online_count", len(onlineMembers),
		"offline_count", len(offlineMembers))
}

// buildGroupPushMessage 构建群消息推送体
func (s *GroupService) buildGroupPushMessage(msg *message.Message) []byte {
	pushMsg := map[string]interface{}{
		"type": "group_message",
		"payload": map[string]interface{}{
			"msg_id":          msg.MsgID,
			"conversation_id": msg.ConversationID,
			"group_id":        msg.GroupID,
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

// 辅助类型定义
type CreateGroupResponse struct {
	GroupID        string   `json:"group_id"`
	Name           string   `json:"name"`
	ConversationID string   `json:"conversation_id"`
	OwnerUserID    string   `json:"owner_user_id"`
	MemberIDs      []string `json:"member_ids"`
	CreatedAt      int64    `json:"created_at"`
}

type SendGroupMessageResponse struct {
	MsgID          string `json:"msg_id"`
	ConversationID string `json:"conversation_id"`
	Status         string `json:"status"`
	ServerTS       int64  `json:"server_ts"`
}

type GroupMemberWithStatus struct {
	UserID   string `json:"user_id"`
	Role     string `json:"role"`
	JoinedAt int64  `json:"joined_at"`
	Online   bool   `json:"online"`
}
