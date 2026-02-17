package message

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
)

// Cursor 游标结构
type Cursor struct {
	TS int64 `json:"ts"` // 时间戳
	ID uint  `json:"id"` // 消息ID
}

// EncodeCursor 编码游标为base64字符串
func EncodeCursor(ts int64, id uint) string {
	c := Cursor{TS: ts, ID: id}
	data, err := json.Marshal(c)
	if err != nil {
		return ""
	}
	return base64.URLEncoding.EncodeToString(data)
}

// DecodeCursor 解码游标字符串
func DecodeCursor(cursorStr string) (int64, uint, error) {
	if cursorStr == "" {
		return 0, 0, nil
	}
	
	data, err := base64.URLEncoding.DecodeString(cursorStr)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid cursor format: %w", err)
	}
	
	var c Cursor
	if err := json.Unmarshal(data, &c); err != nil {
		return 0, 0, fmt.Errorf("invalid cursor data: %w", err)
	}
	
	return c.TS, c.ID, nil
}

// GetMessagesByCursorWithOrder 使用游标分页获取消息（稳定分页）
// direction: "before" 获取历史消息, "after" 获取最新消息
func GetMessagesByCursorWithOrder(conversationID string, cursorTS int64, cursorID uint, limit int, direction string) string {
	// 首次拉取返回空游标
	if cursorTS == 0 && cursorID == 0 {
		return ""
	}
	
	if direction == "after" {
		// 向后翻页，下一个游标应该是最后一条消息
		return EncodeCursor(cursorTS, cursorID)
	}
	
	// 向前翻页，返回最后一条消息作为下一个游标起点
	return EncodeCursor(cursorTS, cursorID)
}
