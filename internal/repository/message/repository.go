package message

import (
	"sort"
	"time"

	msgmodel "PingMe/internal/model/message"

	"gorm.io/gorm"
)

// Repository 消息数据访问层
type Repository struct {
	db *gorm.DB
}

// NewRepository 创建消息仓库
func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// InitSchema 初始化消息相关表
func (r *Repository) InitSchema() error {
	return r.db.AutoMigrate(
		&msgmodel.Message{},
		&msgmodel.Conversation{},
		&msgmodel.ConversationMember{},
	)
}

// CreateMessage 创建消息
func (r *Repository) CreateMessage(msg *msgmodel.Message) error {
	return r.db.Create(msg).Error
}

// GetMessageByMsgID 根据 msgID 获取消息
func (r *Repository) GetMessageByMsgID(msgID string) (*msgmodel.Message, error) {
	var msg msgmodel.Message
	err := r.db.Where("msg_id = ?", msgID).First(&msg).Error
	if err != nil {
		return nil, err
	}
	return &msg, nil
}

// GetMessagesByConversation 获取会话消息历史
func (r *Repository) GetMessagesByConversation(conversationID string, limit int, beforeMsgID string) ([]msgmodel.Message, error) {
	query := r.db.Where("conversation_id = ?", conversationID).
		Order("server_ts DESC, id DESC")

	if beforeMsgID != "" {
		// 获取指定消息的创建时间作为游标
		beforeMsg, err := r.GetMessageByMsgID(beforeMsgID)
		if err == nil {
			query = query.Where("(server_ts < ? OR (server_ts = ? AND id < ?))",
				beforeMsg.ServerTS, beforeMsg.ServerTS, beforeMsg.ID)
		}
	}

	var messages []msgmodel.Message
	err := query.Limit(limit + 1).Find(&messages).Error // 多查一条用于判断是否有更多
	return messages, err
}

// GetMessagesAfter 从某个时间戳获取离线消息
func (r *Repository) GetMessagesAfter(conversationID string, afterTS int64, limit int) ([]msgmodel.Message, error) {
	var messages []msgmodel.Message
	err := r.db.Where("conversation_id = ? AND server_ts > ?", conversationID, afterTS).
		Order("server_ts ASC, id ASC").
		Limit(limit).
		Find(&messages).Error
	return messages, err
}

// GetUserConversations 获取用户的所有会话
func (r *Repository) GetUserConversations(userID string, limit int, cursor string) ([]msgmodel.Conversation, string, error) {
	var members []msgmodel.ConversationMember
	query := r.db.Where("user_id = ?", userID)

	if cursor != "" {
		query = query.Where("created_at < ?", cursor)
	}

	err := query.Order("created_at DESC").Limit(limit + 1).Find(&members).Error
	if err != nil {
		return nil, "", err
	}

	if len(members) == 0 {
		return []msgmodel.Conversation{}, "", nil
	}

	// 获取会话详情
	convIDs := make([]string, len(members))
	for i, m := range members {
		convIDs[i] = m.ConversationID
	}

	var conversations []msgmodel.Conversation
	err = r.db.Where("conversation_id IN ?", convIDs).Find(&conversations).Error
	if err != nil {
		return nil, "", err
	}

	// 按原始顺序排列
	sort.Slice(conversations, func(i, j int) bool {
		return conversations[i].UpdatedAt.After(conversations[j].UpdatedAt)
	})

	newCursor := ""
	if len(conversations) > limit {
		conversations = conversations[:limit]
		newCursor = conversations[limit-1].CreatedAt.Format(time.RFC3339)
	}

	return conversations, newCursor, nil
}

// GetOrCreatePrivateConversation 获取或创建私聊会话
func (r *Repository) GetOrCreatePrivateConversation(userID1, userID2 string) (*msgmodel.Conversation, error) {
	// 生成会话 ID（确保两人一致）
	convID := generatePrivateConversationID(userID1, userID2)

	var conv msgmodel.Conversation
	err := r.db.Where("conversation_id = ? AND type = ?", convID, msgmodel.ConvTypePrivate).First(&conv).Error
	if err == nil {
		return &conv, nil
	}

	if err != gorm.ErrRecordNotFound {
		return nil, err
	}

	// 创建新会话
	conv = msgmodel.Conversation{
		ConversationID: convID,
		Type:            msgmodel.ConvTypePrivate,
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}

	tx := r.db.Begin()
	if err := tx.Create(&conv).Error; err != nil {
		tx.Rollback()
		return nil, err
	}

	// 添加会话成员
	members := []msgmodel.ConversationMember{
		{
			ConversationID: convID,
			UserID:          userID1,
			Role:            "member",
			JoinedAt:        time.Now(),
			CreatedAt:       time.Now(),
			UpdatedAt:       time.Now(),
		},
		{
			ConversationID: convID,
			UserID:          userID2,
			Role:            "member",
			JoinedAt:        time.Now(),
			CreatedAt:       time.Now(),
			UpdatedAt:       time.Now(),
		},
	}

	if err := tx.Create(&members).Error; err != nil {
		tx.Rollback()
		return nil, err
	}

	tx.Commit()
	return &conv, nil
}

// UpdateMessageStatus 更新消息状态
func (r *Repository) UpdateMessageStatus(msgID string, status msgmodel.MessageStatus) error {
	return r.db.Model(&msgmodel.Message{}).Where("msg_id = ?", msgID).Update("status", status).Error
}

// UpdateConversationTime 更新会话最后活跃时间
func (r *Repository) UpdateConversationTime(conversationID string) error {
	return r.db.Model(&msgmodel.Conversation{}).Where("conversation_id = ?", conversationID).
		Update("updated_at", time.Now()).Error
}

// GetConversationByID 根据 ID 获取会话
func (r *Repository) GetConversationByID(conversationID string) (*msgmodel.Conversation, error) {
	var conv msgmodel.Conversation
	err := r.db.Where("conversation_id = ?", conversationID).First(&conv).Error
	if err != nil {
		return nil, err
	}
	return &conv, nil
}

// GetConversationMembers 获取会话成员
func (r *Repository) GetConversationMembers(conversationID string) ([]msgmodel.ConversationMember, error) {
	var members []msgmodel.ConversationMember
	err := r.db.Where("conversation_id = ?", conversationID).Find(&members).Error
	return members, err
}

// IsConversationMember 检查用户是否为会话成员
func (r *Repository) IsConversationMember(conversationID, userID string) (bool, error) {
	var count int64
	err := r.db.Model(&msgmodel.ConversationMember{}).
		Where("conversation_id = ? AND user_id = ?", conversationID, userID).
		Count(&count).Error
	return count > 0, err
}

// GetLastMessage 获取会话最后一条消息
func (r *Repository) GetLastMessage(conversationID string) (*msgmodel.Message, error) {
	var msg msgmodel.Message
	err := r.db.Where("conversation_id = ?", conversationID).
		Order("server_ts DESC, id DESC").
		First(&msg).Error
	if err != nil {
		return nil, err
	}
	return &msg, nil
}

// CountUnread 统计未读消息数
func (r *Repository) CountUnread(conversationID, userID string) (int64, error) {
	var count int64
	// 这里简单实现，实际需要根据 last_read_msg_id 来计算
	err := r.db.Model(&msgmodel.Message{}).
		Where("conversation_id = ? AND to_user_id = ? AND status != ?", 
			conversationID, userID, msgmodel.MsgStatusRead).
		Count(&count).Error
	return count, err
}

// generatePrivateConversationID 生成私聊会话 ID
func generatePrivateConversationID(userID1, userID2 string) string {
	if userID1 < userID2 {
		return "private:" + userID1 + ":" + userID2
	}
	return "private:" + userID2 + ":" + userID1
}
