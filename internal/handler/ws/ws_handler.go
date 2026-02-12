package ws

import (
	"PingMe/internal/config"
	"PingMe/internal/gateway"
	"net/http"

	"github.com/gin-gonic/gin"
)

// Handler handles WebSocket connections
type Handler struct {
	Hub *gateway.Hub
}

// NewHandler creates a new WebSocket handler
func NewHandler(cfg *config.Config, hub *gateway.Hub) *Handler {
	return &Handler{
		Hub: hub,
	}
}

// HandleWebSocket handles WebSocket connection requests
// @Summary WebSocket Connection
// @Description Establish WebSocket connection for IM
// @Tags websocket
// @Produce json
// @Success 101 "Switching Protocols"
// @Router /ws [GET]
func (h *Handler) HandleWebSocket(c *gin.Context) {
	gateway.HandleWebSocket(h.Hub)(c)
}

// GetConnectionStats 获取连接统计信息
func (h *Handler) GetConnectionStats(c *gin.Context) {
	stats := h.Hub.GetStats()

	c.JSON(http.StatusOK, gin.H{
		"status": "success",
		"data":   stats,
	})
}
