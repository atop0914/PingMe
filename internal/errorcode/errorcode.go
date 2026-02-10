package errorcode

import (
	"net/http"
)

// Code defines an error code type
type Code struct {
	HTTPStatus int
	Message    string
	Code       string
}

var (
	// Success 成功
	Success = &Code{
		HTTPStatus: http.StatusOK,
		Message:    "success",
		Code:       "0",
	}

	// ServerError 服务器内部错误
	ServerError = &Code{
		HTTPStatus: http.StatusInternalServerError,
		Message:    "server error",
		Code:       "10001",
	}

	// InvalidParams 参数错误
	InvalidParams = &Code{
		HTTPStatus: http.StatusBadRequest,
		Message:    "invalid parameters",
		Code:       "10002",
	}

	// Unauthorized 未授权
	Unauthorized = &Code{
		HTTPStatus: http.StatusUnauthorized,
		Message:    "unauthorized",
		Code:       "10003",
	}

	// Forbidden 禁止访问
	Forbidden = &Code{
		HTTPStatus: http.StatusForbidden,
		Message:    "forbidden",
		Code:       "10004",
	}

	// NotFound 资源不存在
	NotFound = &Code{
		HTTPStatus: http.StatusNotFound,
		Message:    "not found",
		Code:       "10005",
	}

	// Conflict 冲突
	Conflict = &Code{
		HTTPStatus: http.StatusConflict,
		Message:    "conflict",
		Code:       "10006",
	}

	// TooManyRequests 请求过于频繁
	TooManyRequests = &Code{
		HTTPStatus: http.StatusTooManyRequests,
		Message:    "too many requests",
		Code:       "10007",
	}

	// DatabaseError 数据库错误
	DatabaseError = &Code{
		HTTPStatus: http.StatusInternalServerError,
		Message:    "database error",
		Code:       "20001",
	}

	// CacheError 缓存错误
	CacheError = &Code{
		HTTPStatus: http.StatusInternalServerError,
		Message:    "cache error",
		Code:       "20002",
	}

	// MessageError 消息错误
	MessageError = &Code{
		HTTPStatus: http.StatusInternalServerError,
		Message:    "message processing error",
		Code:       "20003",
	}

	// WebSocketError WebSocket错误
	WebSocketError = &Code{
		HTTPStatus: http.StatusInternalServerError,
		Message:    "websocket error",
		Code:       "20004",
	}

	// UserNotFound 用户不存在
	UserNotFound = &Code{
		HTTPStatus: http.StatusNotFound,
		Message:    "user not found",
		Code:       "30001",
	}

	// UserAlreadyExists 用户已存在
	UserAlreadyExists = &Code{
		HTTPStatus: http.StatusConflict,
		Message:    "user already exists",
		Code:       "30002",
	}

	// InvalidPassword 密码错误
	InvalidPassword = &Code{
		HTTPStatus: http.StatusUnauthorized,
		Message:    "invalid password",
		Code:       "30003",
	}

	// TokenExpired Token过期
	TokenExpired = &Code{
		HTTPStatus: http.StatusUnauthorized,
		Message:    "token expired",
		Code:       "30004",
	}

	// InvalidToken 无效Token
	InvalidToken = &Code{
		HTTPStatus: http.StatusUnauthorized,
		Message:    "invalid token",
		Code:       "30005",
	}

	// ConversationNotFound 会话不存在
	ConversationNotFound = &Code{
		HTTPStatus: http.StatusNotFound,
		Message:    "conversation not found",
		Code:       "40001",
	}

	// GroupNotFound 群组不存在
	GroupNotFound = &Code{
		HTTPStatus: http.StatusNotFound,
		Message:    "group not found",
		Code:       "40002",
	}

	// NotGroupMember 非群成员
	NotGroupMember = &Code{
		HTTPStatus: http.StatusForbidden,
		Message:    "not a group member",
		Code:       "40003",
	}
)

// ToMap converts error code to map for JSON response
func (c *Code) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"code":    c.Code,
		"message": c.Message,
	}
}
