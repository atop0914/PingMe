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
		&msgmodel.UserCursor{},
		&msgmodel.MessageACK{},
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

// GetUserCursor 获取用户会话游标
func (r *Repository) GetUserCursor(userID, conversationID string) (*msgmodel.UserCursor, error) {
	var cursor msgmodel.UserCursor
	err := r.db.Where("user_id = ? AND conversation_id = ?", userID, conversationID).First(&cursor).Error
	if err != nil {
		return nil, err
	}
	return &cursor, nil
}

// UpsertUserCursor 更新或创建用户会话游标
func (r *Repository) UpsertUserCursor(cursor *msgmodel.UserCursor) error {
	return r.db.Where("user_id = ? AND conversation_id = ?", cursor.UserID, cursor.ConversationID).
		Assign(*cursor).
		FirstOrCreate(cursor).Error
}

// GetUserCursors 获取用户所有会话游标
func (r *Repository) GetUserCursors(userID string) ([]msgmodel.UserCursor, error) {
	var cursors []msgmodel.UserCursor
	err := r.db.Where("user_id = ?", userID).Find(&cursors).Error
	return cursors, err
}

// GetUnreadCountByCursor 根据游标计算未读数
func (r *Repository) GetUnreadCountByCursor(userID, conversationID string, lastReadTS int64) (int64, error) {
	var count int64
	err := r.db.Model(&msgmodel.Message{}).
		Where("conversation_id = ? AND from_user_id != ? AND server_ts > ?", 
			conversationID, userID, lastReadTS).
		Count(&count).Error
	return count, err
}

// GetMessagesByCursor 游标分页获取消息（更稳定的分页）
func (r *Repository) GetMessagesByCursor(conversationID string, cursorTS int64, cursorID uint, limit int, isAfter bool) ([]msgmodel.Message, error) {
	query := r.db.Where("conversation_id = ?", conversationID)

	if isAfter {
		// 向后翻页（拉取更新的消息）
		if cursorTS > 0 {
			query = query.Where("server_ts > ? OR (server_ts = ? AND id > ?)", cursorTS, cursorTS, cursorID)
		}
		query = query.Order("server_ts ASC, id ASC")
	} else {
		// 向前翻页（拉取历史消息）
		if cursorTS > 0 {
			query = query.Where("server_ts < ? OR (server_ts = ? AND id < ?)", cursorTS, cursorTS, cursorID)
		} else {
			// 首次拉取，从最新开始
			query = query.Order("server_ts DESC, id DESC")
		}
		if cursorTS == 0 {
			// 首次拉取才需要倒序
			limit = limit + 1
		}
	}

	var messages []msgmodel.Message
	err := query.Limit(limit).Find(&messages).Error
	return messages, err
}

// GetMessagesWithCursor 使用游标拉取离线消息（支持所有会话）
func (r *Repository) GetMessagesWithCursor(userID string, cursorTS int64, cursorID uint, limit int) ([]msgmodel.Message, error) {
	// 获取用户所有会话
	var members []msgmodel.ConversationMember
	err := r.db.Where("user_id = ?", userID).Find(&members).Error
	if err != nil {
		return nil, err
	}

	if len(members) == 0 {
		return []msgmodel.Message{}, nil
	}

	convIDs := make([]string, len(members))
	for i, m := range members {
		convIDs[i] = m.ConversationID
	}

	// 构建查询：获取指定时间之后的所有消息
	var messages []msgmodel.Message
	query := r.db.Where("conversation_id IN ? AND server_ts > ?", convIDs, cursorTS).
		Or("conversation_id IN ? AND server_ts = ? AND id > ?", convIDs, cursorTS, cursorID).
		Order("server_ts ASC, id ASC").
		Limit(limit)

	err = query.Find(&messages).Error
	return messages, err
}

// GetConversationUnreadCounts 获取所有会话的未读数
func (r *Repository) GetConversationUnreadCounts(userID string) (map[string]int64, error) {
	// 获取用户所有会话
	var members []msgmodel.ConversationMember
	err := r.db.Where("user_id = ?", userID).Find(&members).Error
	if err != nil {
		return nil, err
	}

	if len(members) == 0 {
		return make(map[string]int64), nil
	}

	convIDs := make([]string, len(members))
	convMap := make(map[string]string) // convID -> userID (for checking)
	for i, m := range members {
		convIDs[i] = m.ConversationID
		convMap[m.ConversationID] = m.UserID
	}

	// 获取用户的游标
	cursors, err := r.GetUserCursors(userID)
	cursorMap := make(map[string]int64)
	for _, c := range cursors {
		cursorMap[c.ConversationID] = c.LastReadTS
	}

	// 统计每个会话的未读数
	result := make(map[string]int64)
	for _, convID := range convIDs {
		lastReadTS := cursorMap[convID]
		var count int64
		err := r.db.Model(&msgmodel.Message{}).
			Where("conversation_id = ? AND from_user_id != ? AND server_ts > ?", 
				convID, userID, lastReadTS).
			Count(&count).Error
		if err == nil {
			result[convID] = count
		}
	}

	return result, nil
}

// ========== ACK 相关方法 ==========

// CreateOrUpdateACK 创建或更新 ACK（幂等操作）
func (r *Repository) CreateOrUpdateACK(ack *msgmodel.MessageACK) error {
	// 先查询是否已存在
	var existing msgmodel.MessageACK
	err := r.db.Where("msg_id = ? AND user_id = ? AND ack_type = ?", 
		ack.MsgID, ack.UserID, ack.ACKType).First(&existing).Error
	
	if err == nil {
		// 已存在，更新时间戳（幂等：重复 ACK 不改变最终状态）
		if ack.ACKTS > existing.ACKTS {
			return r.db.Model(&existing).Update("ack_ts", ack.ACKTS).Error
		}
		return nil // 重复 ACK，直接返回
	}
	
	if err != gorm.ErrRecordNotFound {
		return err
	}
	
	// 不存在，创建新的
	return r.db.Create(ack).Error
}

// GetACKByMsgIDAndUser 获取指定消息和用户的 ACK
func (r *Repository) GetACKByMsgIDAndUser(msgID, userID string, ackType msgmodel.ACKType) (*msgmodel.MessageACK, error) {
	var ack msgmodel.MessageACK
	err := r.db.Where("msg_id = ? AND user_id = ? AND ack_type = ?", msgID, userID, ackType).First(&ack).Error
	if err != nil {
		return nil, err
	}
	return &ack, nil
}

// GetACKsByConversation 获取会话的所有 ACK（用于补偿）
func (r *Repository) GetACKsByConversation(userID, conversationID string, afterTS int64, ackType msgmodel.ACKType) ([]msgmodel.MessageACK, error) {
	var acks []msgmodel.MessageACK
	query := r.db.Where("user_id = ? AND conversation_id = ? AND ack_type = ?", 
		userID, conversationID, ackType)
	
	if afterTS > 0 {
		query = query.Where("ack_ts > ?", afterTS)
	}
	
	err := query.Order("ack_ts ASC").Find(&acks).Error
	return acks, err
}

// GetLastACK 获取用户对会话的最后 ACK
func (r *Repository) GetLastACK(userID, conversationID string, ackType msgmodel.ACKType) (*msgmodel.MessageACK, error) {
	var ack msgmodel.MessageACK
	err := r.db.Where("user_id = ? AND conversation_id = ? AND ack_type = ?", 
		userID, conversationID, ackType).
		Order("ack_ts DESC").
		First(&ack).Error
	if err != nil {
		return nil, err
	}
	return &ack, nil
}

// DeleteOldACKs 清理过期的 ACK（回执表膨胀处理）
// 保留策略：只保留最近 7 天的已读 ACK，30 天的送达 ACK
func (r *Repository) DeleteOldACKs(retentionDays int) (int64, error) {
	cutoffTime := time.Now().AddDate(0, 0, -retentionDays)
	
	// 删除已读 ACK（7天）
	result := r.db.Where("ack_type = ? AND created_at < ?", msgmodel.ACKTypeRead, cutoffTime).Delete(&msgmodel.MessageACK{})
	if result.Error != nil {
		return 0, result.Error
	}
	
	readCount := result.RowsAffected
	
	// 删除送达 ACK（30天）
	cutoffTime = time.Now().AddDate(0, 0, -30)
	result = r.db.Where("ack_type = ? AND created_at < ?", msgmodel.ACKTypeDelivered, cutoffTime).Delete(&msgmodel.MessageACK{})
	if result.Error != nil {
		return readCount, result.Error
	}
	
	return readCount + result.RowsAffected, nil
}

// GetACKCount 获取 ACK 数量（用于监控）
func (r *Repository) GetACKCount() (int64, error) {
	var count int64
	err := r.db.Model(&msgmodel.MessageACK{}).Count(&count).Error
	return count, err
}

// GetACKCountByType 按类型统计 ACK 数量
func (r *Repository) GetACKCountByType(ackType msgmodel.ACKType) (int64, error) {
	var count int64
	err := r.db.Model(&msgmodel.MessageACK{}).Where("ack_type = ?", ackType).Count(&count).Error
	return count, err
}

// generatePrivateConversationID 生成私聊会话 ID
func generatePrivateConversationID(userID1, userID2 string) string {
	if userID1 < userID2 {
		return "private:" + userID1 + ":" + userID2
	}
	return "private:" + userID2 + ":" + userID1
}

// ========== 群组相关方法 ==========

// CreateGroup 创建群组
func (r *Repository) CreateGroup(conversationID, groupID, groupName, ownerUserID string, members []msgmodel.ConversationMember) error {
	tx := r.db.Begin()

	// 创建群会话
	conv := msgmodel.Conversation{
		ConversationID: conversationID,
		Type:           msgmodel.ConvTypeGroup,
		Name:           groupName,
		OwnerUserID:    ownerUserID,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	if err := tx.Create(&conv).Error; err != nil {
		tx.Rollback()
		return err
	}

	// 添加成员
	if err := tx.Create(&members).Error; err != nil {
		tx.Rollback()
		return err
	}

	tx.Commit()
	return nil
}

// GetGroupByGroupID 根据 groupID 获取群会话
func (r *Repository) GetGroupByGroupID(groupID string) (*msgmodel.Conversation, error) {
	conversationID := "group:" + groupID
	var conv msgmodel.Conversation
	err := r.db.Where("conversation_id = ? AND type = ?", conversationID, msgmodel.ConvTypeGroup).First(&conv).Error
	if err != nil {
		return nil, err
	}
	return &conv, nil
}

// AddGroupMember 添加群成员
func (r *Repository) AddGroupMember(member *msgmodel.ConversationMember) error {
	return r.db.Create(member).Error
}

// RemoveGroupMember 移除群成员
func (r *Repository) RemoveGroupMember(conversationID, userID string) error {
	return r.db.Where("conversation_id = ? AND user_id = ?", conversationID, userID).
		Delete(&msgmodel.ConversationMember{}).Error
}

// GetGroupMembers 获取群成员列表
func (r *Repository) GetGroupMembers(groupID string) ([]msgmodel.ConversationMember, error) {
	conversationID := "group:" + groupID
	var members []msgmodel.ConversationMember
	err := r.db.Where("conversation_id = ?", conversationID).Find(&members).Error
	return members, err
}

// GetGroupMembersByConversationID 根据会话ID获取群成员
func (r *Repository) GetGroupMembersByConversationID(conversationID string) ([]msgmodel.ConversationMember, error) {
	var members []msgmodel.ConversationMember
	err := r.db.Where("conversation_id = ?", conversationID).Find(&members).Error
	return members, err
}
