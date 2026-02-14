package gateway

import (
	"fmt"
	"sync"
	"time"

	"PingMe/internal/logger"
)

// MessageDeduplicator 消息去重器
type MessageDeduplicator struct {
	// 已确认的消息ID集合（用户ID -> 消息ID）
	confirmedMessages map[string]map[string]bool
	// 待确认的消息（用于去重）
	pendingMessages   map[string]map[string]time.Time
	mu                sync.RWMutex
	// 消息有效期（5分钟）
	messageTTL        time.Duration
	// 清理间隔
	cleanupInterval   time.Duration
	stopCh            chan struct{}
}

// NewMessageDeduplicator 创建消息去重器
func NewMessageDeduplicator() *MessageDeduplicator {
	d := &MessageDeduplicator{
		confirmedMessages: make(map[string]map[string]bool),
		pendingMessages:  make(map[string]map[string]time.Time),
		messageTTL:        5 * time.Minute,
		cleanupInterval:  1 * time.Minute,
		stopCh:           make(chan struct{}),
	}

	// 启动清理任务
	go d.cleanupLoop()

	return d
}

// Stop 停止去重器
func (d *MessageDeduplicator) Stop() {
	close(d.stopCh)
}

// IsDuplicate 检查消息是否重复
// 返回值: isDuplicate(是否重复), isPending(是否在待确认中)
func (d *MessageDeduplicator) IsDuplicate(userID, msgID string) (bool, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	// 检查已确认消息
	if confirmed, ok := d.confirmedMessages[userID]; ok {
		if confirmed[msgID] {
			return true, false
		}
	}

	// 检查待确认消息
	if pending, ok := d.pendingMessages[userID]; ok {
		if _, exists := pending[msgID]; exists {
			return true, true
		}
	}

	return false, false
}

// MarkPending 将消息标记为待确认
func (d *MessageDeduplicator) MarkPending(userID, msgID string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if _, ok := d.pendingMessages[userID]; !ok {
		d.pendingMessages[userID] = make(map[string]time.Time)
	}
	d.pendingMessages[userID][msgID] = time.Now()
}

// ConfirmMessage 确认消息（投递成功）
func (d *MessageDeduplicator) ConfirmMessage(userID, msgID string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	// 从待确认中移除
	if pending, ok := d.pendingMessages[userID]; ok {
		delete(pending, msgID)
	}

	// 添加到已确认
	if _, ok := d.confirmedMessages[userID]; !ok {
		d.confirmedMessages[userID] = make(map[string]bool)
	}
	d.confirmedMessages[userID][msgID] = true
}

// GetLastSeq 获取用户最后确认的消息序列号
func (d *MessageDeduplicator) GetLastSeq(userID string) int64 {
	d.mu.RLock()
	defer d.mu.RUnlock()

	// 这里简化处理，实际可以使用更复杂的序列号
	// 返回 0 表示从最新消息开始
	return 0
}

// cleanupLoop 定期清理过期消息
func (d *MessageDeduplicator) cleanupLoop() {
	ticker := time.NewTicker(d.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-d.stopCh:
			return
		case <-ticker.C:
			d.cleanup()
		}
	}
}

// cleanup 清理过期消息
func (d *MessageDeduplicator) cleanup() {
	d.mu.Lock()
	defer d.mu.Unlock()

	now := time.Now()

	// 清理待确认消息
	for uid, pending := range d.pendingMessages {
		for msgID, ts := range pending {
			if now.Sub(ts) > d.messageTTL {
				delete(pending, msgID)
			}
		}
		// 如果用户没有待确认消息，删除用户条目
		if len(pending) == 0 {
			delete(d.pendingMessages, uid)
		}
	}

	// 清理已确认消息（限制每个用户保留最近1000条）
	for uid, confirmed := range d.confirmedMessages {
		if len(confirmed) > 1000 {
			// 保留最新的1000条
			count := 0
			for msgID := range confirmed {
				count++
				if count > 1000 {
					delete(confirmed, msgID)
				}
			}
			_ = uid // 避免编译器警告
		}
	}

	logger.Debug("Message deduplicator cleaned up")
}

// ConversationCursor 会话游标
type ConversationCursor struct {
	UserID         string `json:"user_id"`
	ConversationID string `json:"conversation_id"`
	LastMsgID      string `json:"last_msg_id"`      // 最后一条消息的ID
	LastMsgTS      int64  `json:"last_msg_ts"`      // 最后一条消息的时间戳
	LastServerSeq  int64  `json:"last_server_seq"` // 服务端消息序列号
}

// CursorManager 游标管理器
type CursorManager struct {
	// 用户会话游标 (userID -> conversationID -> cursor)
	cursors map[string]map[string]*ConversationCursor
	mu      sync.RWMutex
}

// NewCursorManager 创建游标管理器
func NewCursorManager() *CursorManager {
	return &CursorManager{
		cursors: make(map[string]map[string]*ConversationCursor),
	}
}

// GetCursor 获取游标
func (cm *CursorManager) GetCursor(userID, conversationID string) *ConversationCursor {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if convCursors, ok := cm.cursors[userID]; ok {
		return convCursors[conversationID]
	}
	return nil
}

// UpdateCursor 更新游标
func (cm *CursorManager) UpdateCursor(userID, conversationID string, cursor *ConversationCursor) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if _, ok := cm.cursors[userID]; !ok {
		cm.cursors[userID] = make(map[string]*ConversationCursor)
	}
	cm.cursors[userID][conversationID] = cursor
}

// DeleteCursor 删除游标
func (cm *CursorManager) DeleteCursor(userID, conversationID string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if convCursors, ok := cm.cursors[userID]; ok {
		delete(convCursors, conversationID)
		if len(convCursors) == 0 {
			delete(cm.cursors, userID)
		}
	}
}

// GetOfflineMessagesParams 获取离线消息参数
type GetOfflineMessagesParams struct {
	UserID         string `json:"user_id"`
	ConversationID string `json:"conversation_id,omitempty"` // 空表示所有会话
	LastMsgID      string `json:"last_msg_id,omitempty"`
	LastMsgTS      int64  `json:"last_msg_ts,omitempty"`
	Limit          int    `json:"limit"`
}

// Validate 验证参数
func (p *GetOfflineMessagesParams) Validate() error {
	if p.UserID == "" {
		return fmt.Errorf("user_id is required")
	}
	if p.Limit <= 0 || p.Limit > 100 {
		p.Limit = 20 // 默认值
	}
	return nil
}
