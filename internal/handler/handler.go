package handler

import (
	"net/http"
	"runtime"

	"PingMe/internal/config"
	"PingMe/pkg/response"

	"github.com/gin-gonic/gin"
)

// BuildInfo holds build information
type BuildInfo struct {
	Version   string `json:"version"`
	GoVersion string `json:"go_version"`
	Commit    string `json:"commit,omitempty"`
	Date      string `json:"date,omitempty"`
}

// Handler holds the application handlers
type Handler struct {
	cfg *config.Config
}

// NewHandler creates a new Handler instance
func NewHandler(cfg *config.Config) *Handler {
	return &Handler{cfg: cfg}
}

// HealthCheck handles health check requests
// @Summary Health check
// @Description Check if the service is healthy
// @Tags health
// @Accept json
// @Produce json
// @Success 200 {object} response.Response
// @Router /health [get]
func (h *Handler) HealthCheck(c *gin.Context) {
	c.JSON(http.StatusOK, response.Success(map[string]interface{}{
		"status":  "healthy",
		"service": h.cfg.App.Name,
		"env":     h.cfg.App.Env,
	}))
}

// VersionInfo handles version information requests
// @Summary Version info
// @Description Get service version information
// @Tags info
// @Accept json
// @Produce json
// @Success 200 {object} response.Response
// @Router /version [get]
func (h *Handler) VersionInfo(c *gin.Context) {
	info := BuildInfo{
		Version:   "1.0.0",
		GoVersion: runtime.Version(),
	}

	c.JSON(http.StatusOK, response.Success(map[string]interface{}{
		"build": info,
		"app":   h.cfg.App.Name,
	}))
}
