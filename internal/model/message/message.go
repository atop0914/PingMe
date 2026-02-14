package message

import (
	"time"

	"github.com/google/uuid"
)

// MessageType 消息内容类型
type MessageType string

const (
	MsgContentTypeText    MessageType = "text"
	MsgContentTypeImage  MessageType = "image"
	MsgContentTypeFile   MessageType = "file"
	MsgContentTypeVoice  MessageType = "voice"
)

// MessageStatus 消息状态
type MessageStatus string

const (
	MsgStatusSending   MessageStatus = "sending"
	MsgStatusSent      MessageStatus = "sent"
	MsgStatusDelivered MessageStatus = "delivered"
	MsgStatusRead      MessageStatus = "read"
	MsgStatusRecall    MessageStatus = "recall"
)

// ConversationType 会话类型
type ConversationType string

const (
	ConvTypePrivate ConversationType = "private"
	ConvTypeGroup   ConversationType = "group"
)

// Message 消息模型
type Message struct {
	ID             uint           `json:"id" gorm:"primaryKey"`
	MsgID          string         `json:"msg_id" gorm:"type:varchar(64);uniqueIndex;not null"`
	ConversationID string         `json:"conversation_id" gorm:"type:varchar(128);index;not null"`
	FromUserID     string         `json:"from_user_id" gorm:"type:varchar(36);index;not null"`
	ToUserID       string         `json:"to_user_id" gorm:"type:varchar(36);index"` // 单聊时为目标用户ID
	GroupID        string         `json:"group_id" gorm:"type:varchar(36);index"`    // 群聊时为群ID
	Content        string         `json:"content" gorm:"type:text;not null"`
	ContentType    MessageType    `json:"content_type" gorm:"type:varchar(20);default:text"`
	Status         MessageStatus `json:"status" gorm:"type:varchar(20);default:sending"`
	ClientTS       int64          `json:"client_ts"` // 客户端时间戳
	ServerTS       int64          `json:"server_ts"` // 服务端时间戳
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

// Conversation 会话模型
type Conversation struct {
	ID             uint              `json:"id" gorm:"primaryKey"`
	ConversationID string           `json:"conversation_id" gorm:"type:varchar(128);uniqueIndex;not null"`
	Type           ConversationType `json:"type" gorm:"type:varchar(20);not null"`
	Name           string            `json:"name" gorm:"type:varchar(100)"` // 群名称
	OwnerUserID    string            `json:"owner_user_id" gorm:"type:varchar(36)"` // 群主
	AvatarURL      string            `json:"avatar_url" gorm:"type:varchar(500)"`
	CreatedAt      time.Time         `json:"created_at"`
	UpdatedAt      time.Time         `json:"updated_at"`
}

// ConversationMember 会话成员
type ConversationMember struct {
	ID             uint      `json:"id" gorm:"primaryKey"`
	ConversationID string   `json:"conversation_id" gorm:"type:varchar(128);index;not null"`
	UserID         string   `json:"user_id" gorm:"type:varchar(36);index;not null"`
	Role           string   `json:"role" gorm:"type:varchar(20);default:member"` // owner/admin/member
	JoinedAt       time.Time `json:"joined_at"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// BeforeCreate 生成消息 ID
func (m *Message) BeforeCreate() error {
	if m.MsgID == "" {
		m.MsgID = uuid.New().String()
	}
	if m.ServerTS == 0 {
		m.ServerTS = time.Now().UnixMilli()
	}
	return nil
}

// SendMessageRequest 发送消息请求
type SendMessageRequest struct {
	ToUserID   string      `json:"to_user_id" binding:"required"`
	Content    string      `json:"content" binding:"required"`
	ContentType MessageType `json:"content_type"`
	ClientTS   int64       `json:"client_ts"`
}

// SendMessageResponse 发送消息响应
type SendMessageResponse struct {
	MsgID          string         `json:"msg_id"`
	ConversationID string         `json:"conversation_id"`
	Status         MessageStatus  `json:"status"`
	ServerTS       int64          `json:"server_ts"`
}

// GetHistoryRequest 获取历史消息请求
type GetHistoryRequest struct {
	ConversationID string `form:"conversation_id" binding:"required"`
	Limit          int    `form:"limit,default=20"`
	BeforeMsgID    string `form:"before_msg_id"`
}

// GetHistoryResponse 获取历史消息响应
type GetHistoryResponse struct {
	Messages       []Message `json:"messages"`
	HasMore        bool      `json:"has_more"`
	NextCursor     string    `json:"next_cursor,omitempty"`
}

// GetConversationsRequest 获取会话列表请求
type GetConversationsRequest struct {
	Limit  int `form:"limit,default=20"`
	Cursor string `form:"cursor"`
}

// GetConversationsResponse 获取会话列表响应
type GetConversationsResponse struct {
	Conversations []ConversationWithLastMessage `json:"conversations"`
	HasMore       bool                         `json:"has_more"`
	NextCursor    string                       `json:"next_cursor,omitempty"`
}

// ConversationWithLastMessage 带最新消息的会话
type ConversationWithLastMessage struct {
	Conversation
	LastMessage   *Message `json:"last_message,omitempty"`
	UnreadCount  int      `json:"unread_count"`
}

// CreateConversationRequest 创建会话请求
type CreateConversationRequest struct {
	Type      ConversationType `json:"type" binding:"required"`
	ToUserID  string           `json:"to_user_id"` // 私聊时使用
	Name      string           `json:"name"`       // 群名称
	MemberIDs []string         `json:"member_ids"` // 群成员
}
