package group

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

// GroupHandler 群组处理器
type GroupHandler struct {
	groupService *service.GroupService
}

// NewGroupHandler 创建群组处理器
func NewGroupHandler(groupService *service.GroupService) *GroupHandler {
	return &GroupHandler{
		groupService: groupService,
	}
}

// CreateGroupRequest 创建群组请求
type CreateGroupRequest struct {
	Name      string   `json:"name" binding:"required"`
	MemberIDs []string `json:"member_ids"` // 初始成员（不包括创建者）
}

// CreateGroupResponse 创建群组响应
type CreateGroupResponse struct {
	GroupID        string   `json:"group_id"`
	Name           string   `json:"name"`
	ConversationID string   `json:"conversation_id"`
	OwnerUserID    string   `json:"owner_user_id"`
	MemberIDs      []string `json:"member_ids"`
	CreatedAt      int64    `json:"created_at"`
}

// CreateGroup 创建群组
// @Summary 创建群组
// @Tags groups
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body CreateGroupRequest true "群组信息"
// @Success 200 {object} response.Response{data=CreateGroupResponse}
// @Router /api/v1/groups [post]
func (h *GroupHandler) CreateGroup(c *gin.Context) {
	claims := middleware.GetUserClaims(c)
	if claims == nil {
		response.Error(c, http.StatusUnauthorized, "Unauthorized")
		return
	}
	userID := claims.UserID

	var req CreateGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "Invalid request: "+err.Error())
		return
	}

	// 群名称不能为空
	if req.Name == "" {
		response.Error(c, http.StatusBadRequest, "Group name is required")
		return
	}

	resp, err := h.groupService.CreateGroup(c.Request.Context(), userID, req.Name, req.MemberIDs)
	if err != nil {
		logger.Error("Failed to create group",
			"owner_user_id", userID,
			"name", req.Name,
			"error", err)
		response.Error(c, http.StatusInternalServerError, "Failed to create group: "+err.Error())
		return
	}

	response.SuccessGin(c, resp)
}

// JoinGroupRequest 加群请求
type JoinGroupRequest struct {
	GroupID string `json:"group_id" binding:"required"`
}

// JoinGroup 加群
// @Summary 加群
// @Tags groups
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body JoinGroupRequest true "加群请求"
// @Success 200 {object} response.Response
// @Router /api/v1/groups/join [post]
func (h *GroupHandler) JoinGroup(c *gin.Context) {
	claims := middleware.GetUserClaims(c)
	if claims == nil {
		response.Error(c, http.StatusUnauthorized, "Unauthorized")
		return
	}
	userID := claims.UserID

	var req JoinGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "Invalid request: "+err.Error())
		return
	}

	err := h.groupService.JoinGroup(c.Request.Context(), userID, req.GroupID)
	if err != nil {
		logger.Error("Failed to join group",
			"user_id", userID,
			"group_id", req.GroupID,
			"error", err)
		response.Error(c, http.StatusInternalServerError, "Failed to join group: "+err.Error())
		return
	}

	response.SuccessGin(c, gin.H{"message": "joined successfully"})
}

// LeaveGroupRequest 退群请求
type LeaveGroupRequest struct {
	GroupID string `json:"group_id" binding:"required"`
}

// LeaveGroup 退群
// @Summary 退群
// @Tags groups
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body LeaveGroupRequest true "退群请求"
// @Success 200 {object} response.Response
// @Router /api/v1/groups/leave [post]
func (h *GroupHandler) LeaveGroup(c *gin.Context) {
	claims := middleware.GetUserClaims(c)
	if claims == nil {
		response.Error(c, http.StatusUnauthorized, "Unauthorized")
		return
	}
	userID := claims.UserID

	var req LeaveGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "Invalid request: "+err.Error())
		return
	}

	err := h.groupService.LeaveGroup(c.Request.Context(), userID, req.GroupID)
	if err != nil {
		logger.Error("Failed to leave group",
			"user_id", userID,
			"group_id", req.GroupID,
			"error", err)
		response.Error(c, http.StatusInternalServerError, "Failed to leave group: "+err.Error())
		return
	}

	response.SuccessGin(c, gin.H{"message": "left successfully"})
}

// GetGroupMembers 获取群成员列表
// @Summary 获取群成员列表
// @Tags groups
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param group_id path string true "群组ID"
// @Success 200 {object} response.Response{data=[]message.ConversationMember}
// @Router /api/v1/groups/{group_id}/members [get]
func (h *GroupHandler) GetGroupMembers(c *gin.Context) {
	claims := middleware.GetUserClaims(c)
	if claims == nil {
		response.Error(c, http.StatusUnauthorized, "Unauthorized")
		return
	}
	userID := claims.UserID

	groupID := c.Param("group_id")
	if groupID == "" {
		response.Error(c, http.StatusBadRequest, "group_id is required")
		return
	}

	// 验证用户是否为群成员
	isMember, err := h.groupService.IsGroupMember(groupID, userID)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "Failed to check membership")
		return
	}
	if !isMember {
		response.Error(c, http.StatusForbidden, "Not a group member")
		return
	}

	members, err := h.groupService.GetGroupMembers(groupID)
	if err != nil {
		logger.Error("Failed to get group members",
			"group_id", groupID,
			"error", err)
		response.Error(c, http.StatusInternalServerError, "Failed to get group members: "+err.Error())
		return
	}

	response.SuccessGin(c, members)
}

// GetUserGroups 获取用户加入的群组列表
// @Summary 获取用户加入的群组列表
// @Tags groups
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param limit query int false "返回数量，默认20"
// @Param cursor query string false "游标"
// @Success 200 {object} response.Response{data=message.GetConversationsResponse}
// @Router /api/v1/groups [get]
func (h *GroupHandler) GetUserGroups(c *gin.Context) {
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

	groups, err := h.groupService.GetUserGroups(c.Request.Context(), userID, limit, cursor)
	if err != nil {
		logger.Error("Failed to get user groups",
			"user_id", userID,
			"error", err)
		response.Error(c, http.StatusInternalServerError, "Failed to get groups: "+err.Error())
		return
	}

	response.SuccessGin(c, groups)
}

// GetGroupInfo 获取群组详情
// @Summary 获取群组详情
// @Tags groups
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param group_id path string true "群组ID"
// @Success 200 {object} response.Response{data=message.Conversation}
// @Router /api/v1/groups/{group_id} [get]
func (h *GroupHandler) GetGroupInfo(c *gin.Context) {
	claims := middleware.GetUserClaims(c)
	if claims == nil {
		response.Error(c, http.StatusUnauthorized, "Unauthorized")
		return
	}
	userID := claims.UserID

	groupID := c.Param("group_id")
	if groupID == "" {
		response.Error(c, http.StatusBadRequest, "group_id is required")
		return
	}

	// 验证用户是否为群成员
	isMember, err := h.groupService.IsGroupMember(groupID, userID)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "Failed to check membership")
		return
	}
	if !isMember {
		response.Error(c, http.StatusForbidden, "Not a group member")
		return
	}

	group, err := h.groupService.GetGroupInfo(groupID)
	if err != nil {
		logger.Error("Failed to get group info",
			"group_id", groupID,
			"error", err)
		response.Error(c, http.StatusNotFound, "Group not found")
		return
	}

	response.SuccessGin(c, group)
}

// SendGroupMessageRequest 发送群消息请求
type SendGroupMessageRequest struct {
	GroupID     string   `json:"group_id" binding:"required"`
	Content     string   `json:"content" binding:"required"`
	ContentType string   `json:"content_type"`
	ClientTS    int64    `json:"client_ts"`
}

// SendGroupMessageResponse 发送群消息响应
type SendGroupMessageResponse struct {
	MsgID          string `json:"msg_id"`
	ConversationID string `json:"conversation_id"`
	Status         string `json:"status"`
	ServerTS       int64  `json:"server_ts"`
}

// SendGroupMessage 发送群消息
// @Summary 发送群消息
// @Tags groups
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body SendGroupMessageRequest true "消息内容"
// @Success 200 {object} response.Response{data=SendGroupMessageResponse}
// @Router /api/v1/groups/messages [post]
func (h *GroupHandler) SendGroupMessage(c *gin.Context) {
	claims := middleware.GetUserClaims(c)
	if claims == nil {
		response.Error(c, http.StatusUnauthorized, "Unauthorized")
		return
	}
	userID := claims.UserID

	var req SendGroupMessageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "Invalid request: "+err.Error())
		return
	}

	// 验证用户是否为群成员
	isMember, err := h.groupService.IsGroupMember(req.GroupID, userID)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "Failed to check membership")
		return
	}
	if !isMember {
		response.Error(c, http.StatusForbidden, "Not a group member")
		return
	}

	// 设置默认内容类型
	contentType := message.MsgContentTypeText
	if req.ContentType != "" {
		contentType = message.MessageType(req.ContentType)
	}

	resp, err := h.groupService.SendGroupMessage(c.Request.Context(), userID, req.GroupID, req.Content, contentType, req.ClientTS)
	if err != nil {
		logger.Error("Failed to send group message",
			"from_user_id", userID,
			"group_id", req.GroupID,
			"error", err)
		response.Error(c, http.StatusInternalServerError, "Failed to send group message: "+err.Error())
		return
	}

	response.SuccessGin(c, resp)
}

// GetGroupMembersWebSocket 获取群成员列表（通过 WebSocket）
// @Summary 获取群成员列表
// @Tags groups
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param group_id path string true "群组ID"
// @Param limit query int false "成员数量限制"
// @Success 200 {object} response.Response{data=GroupMembersWithStatus}
// @Router /api/v1/groups/{group_id}/members/status [get]
func (h *GroupHandler) GetGroupMembersWebSocket(c *gin.Context) {
	claims := middleware.GetUserClaims(c)
	if claims == nil {
		response.Error(c, http.StatusUnauthorized, "Unauthorized")
		return
	}
	userID := claims.UserID

	groupID := c.Param("group_id")
	if groupID == "" {
		response.Error(c, http.StatusBadRequest, "group_id is required")
		return
	}

	// 验证用户是否为群成员
	isMember, err := h.groupService.IsGroupMember(groupID, userID)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "Failed to check membership")
		return
	}
	if !isMember {
		response.Error(c, http.StatusForbidden, "Not a group member")
		return
	}

	limitStr := c.DefaultQuery("limit", "100")
	limit, _ := strconv.Atoi(limitStr)
	if limit <= 0 || limit > 500 {
		limit = 100
	}

	members, err := h.groupService.GetGroupMembersWithStatus(c.Request.Context(), groupID, limit)
	if err != nil {
		logger.Error("Failed to get group members with status",
			"group_id", groupID,
			"error", err)
		response.Error(c, http.StatusInternalServerError, "Failed to get group members: "+err.Error())
		return
	}

	response.SuccessGin(c, members)
}
