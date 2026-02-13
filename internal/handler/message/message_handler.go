package handler

import (
	"net/http"
	"strconv"

	"PingMe/internal/logger"
	"PingMe/internal/middleware"
	"PingMe/internal/model/message"
	"PingMe/internal/pkg/response"
	"PingMe/internal/service"

	"github.com/gin-gonic/gin"
)

// MessageHandler 消息处理器
type MessageHandler struct {
	msgService *service.MessageService
}

// NewMessageHandler 创建消息处理器
func NewMessageHandler(msgService *service.MessageService) *MessageHandler {
	return &MessageHandler{
		msgService: msgService,
	}
}

// SendMessage 发送消息
// @Summary 发送私聊消息
// @Tags messages
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param message body message.SendMessageRequest true "消息内容"
// @Success 200 {object} response.Response{data=message.SendMessageResponse}
// @Router /api/v1/messages [post]
func (h *MessageHandler) SendMessage(c *gin.Context) {
	// 从上下文获取用户信息（已通过 JWT 中间件验证）
	claims := middleware.GetUserClaims(c)
	if claims == nil {
		response.Error(c, http.StatusUnauthorized, "Unauthorized")
		return
	}
	userID := claims.UserID

	var req message.SendMessageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "Invalid request: "+err.Error())
		return
	}

	// 不能给自己发消息
	if req.ToUserID == userID {
		response.Error(c, http.StatusBadRequest, "Cannot send message to yourself")
		return
	}

	// 设置默认内容类型
	if req.ContentType == "" {
		req.ContentType = message.MsgContentTypeText
	}

	// 设置客户端时间戳
	if req.ClientTS == 0 {
		req.ClientTS = 0 // 0 表示使用服务器时间
	}

	resp, err := h.msgService.SendMessage(c.Request.Context(), userID, &req)
	if err != nil {
		logger.Error("Failed to send message",
			"from_user_id", userID,
			"error", err)
		response.Error(c, http.StatusInternalServerError, "Failed to send message")
		return
	}

	response.SuccessGin(c, resp)
}

// GetHistory 获取历史消息
// @Summary 获取会话历史消息
// @Tags messages
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param conversation_id query string true "会话ID"
// @Param limit query int false "返回数量，默认20"
// @Param before_msg_id query string false "游标：上一页最后一条消息的ID"
// @Success 200 {object} response.Response{data=message.GetHistoryResponse}
// @Router /api/v1/messages/history [get]
func (h *MessageHandler) GetHistory(c *gin.Context) {
	claims := middleware.GetUserClaims(c)
	if claims == nil {
		response.Error(c, http.StatusUnauthorized, "Unauthorized")
		return
	}
	userID := claims.UserID

	conversationID := c.Query("conversation_id")
	if conversationID == "" {
		response.Error(c, http.StatusBadRequest, "conversation_id is required")
		return
	}

	limitStr := c.DefaultQuery("limit", "20")
	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit <= 0 || limit > 100 {
		limit = 20
	}

	beforeMsgID := c.Query("before_msg_id")

	resp, err := h.msgService.GetHistory(c.Request.Context(), userID, conversationID, limit, beforeMsgID)
	if err != nil {
		logger.Error("Failed to get history",
			"user_id", userID,
			"conversation_id", conversationID,
			"error", err)
		response.Error(c, http.StatusInternalServerError, "Failed to get history: "+err.Error())
		return
	}

	response.SuccessGin(c, resp)
}

// GetConversations 获取会话列表
// @Summary 获取用户的会话列表
// @Tags conversations
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param limit query int false "返回数量，默认20"
// @Param cursor query string false "游标"
// @Success 200 {object} response.Response{data=message.GetConversationsResponse}
// @Router /api/v1/conversations [get]
func (h *MessageHandler) GetConversations(c *gin.Context) {
	claims := middleware.GetUserClaims(c)
	if claims == nil {
		response.Error(c, http.StatusUnauthorized, "Unauthorized")
		return
	}
	userID := claims.UserID

	limitStr := c.DefaultQuery("limit", "20")
	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit <= 0 || limit > 100 {
		limit = 20
	}

	cursor := c.Query("cursor")

	resp, err := h.msgService.GetConversations(c.Request.Context(), userID, limit, cursor)
	if err != nil {
		logger.Error("Failed to get conversations",
			"user_id", userID,
			"error", err)
		response.Error(c, http.StatusInternalServerError, "Failed to get conversations")
		return
	}

	response.SuccessGin(c, resp)
}

// GetConversation 获取会话详情
// @Summary 获取指定会话详情
// @Tags conversations
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "会话ID"
// @Success 200 {object} response.Response{data=message.Conversation}
// @Router /api/v1/conversations/{id} [get]
func (h *MessageHandler) GetConversation(c *gin.Context) {
	claims := middleware.GetUserClaims(c)
	if claims == nil {
		response.Error(c, http.StatusUnauthorized, "Unauthorized")
		return
	}
	userID := claims.UserID

	conversationID := c.Param("id")
	if conversationID == "" {
		response.Error(c, http.StatusBadRequest, "conversation_id is required")
		return
	}

	// 验证权限
	isMember, err := h.msgService.IsMember(conversationID, userID)
	if err != nil || !isMember {
		response.Error(c, http.StatusForbidden, "Access denied")
		return
	}

	conv, err := h.msgService.GetConversation(conversationID)
	if err != nil {
		response.Error(c, http.StatusNotFound, "Conversation not found")
		return
	}

	response.SuccessGin(c, conv)
}

// PullOfflineMessages 拉取离线消息
// @Summary 拉取离线消息
// @Tags messages
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param last_ts query int64 true "最后收到消息的时间戳(毫秒)"
// @Param limit query int false "每会话最大消息数，默认20"
// @Success 200 {object} response.Response{data=[]message.Message}
// @Router /api/v1/messages/offline [get]
func (h *MessageHandler) PullOfflineMessages(c *gin.Context) {
	claims := middleware.GetUserClaims(c)
	if claims == nil {
		response.Error(c, http.StatusUnauthorized, "Unauthorized")
		return
	}
	userID := claims.UserID

	lastTSStr := c.Query("last_ts")
	lastTS, err := strconv.ParseInt(lastTSStr, 10, 64)
	if err != nil {
		response.Error(c, http.StatusBadRequest, "Invalid last_ts")
		return
	}

	limitStr := c.DefaultQuery("limit", "20")
	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit <= 0 || limit > 100 {
		limit = 20
	}

	messages, err := h.msgService.PullOfflineMessages(c.Request.Context(), userID, lastTS, limit)
	if err != nil {
		logger.Error("Failed to pull offline messages",
			"user_id", userID,
			"error", err)
		response.Error(c, http.StatusInternalServerError, "Failed to pull offline messages")
		return
	}

	response.SuccessGin(c, messages)
}
