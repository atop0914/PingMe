package userprofile

import (
	"net/http"

	"PingMe/internal/errorcode"
	"PingMe/internal/middleware"
	"PingMe/internal/model/user"
	"PingMe/internal/service"
	"PingMe/pkg/response"

	"github.com/gin-gonic/gin"
)

// Handler handles user requests
type Handler struct {
	userSvc *service.Service
}

// NewHandler creates a new user handler
func NewHandler(userSvc *service.Service) *Handler {
	return &Handler{
		userSvc: userSvc,
	}
}

// GetProfile retrieves the current user's profile
// @Summary Get user profile
// @Description Get the profile of the authenticated user
// @Tags user
// @Accept json
// @Produce json
// @Security BearerAuth
// @Success 200 {object} response.Response
// @Router /user/profile [get]
func (h *Handler) GetProfile(c *gin.Context) {
	claims := middleware.GetUserClaims(c)
	if claims == nil {
		c.JSON(http.StatusUnauthorized, response.Fail(errorcode.InvalidToken))
		return
	}

	profile, err := h.userSvc.GetProfile(claims.UserID)
	if err != nil {
		errMsg := err.Error()
		switch errMsg {
		case errorcode.UserNotFound.Message:
			c.JSON(errorcode.UserNotFound.HTTPStatus, response.Fail(errorcode.UserNotFound))
		default:
			c.JSON(errorcode.ServerError.HTTPStatus, response.Fail(errorcode.ServerError))
		}
		return
	}

	c.JSON(http.StatusOK, response.Success(map[string]interface{}{
		"profile": profile,
	}))
}

// UpdateProfile updates the current user's profile
// @Summary Update user profile
// @Description Update the profile of the authenticated user
// @Tags user
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body user.UpdateProfileRequest true "Profile update data"
// @Success 200 {object} response.Response
// @Router /user/profile [put]
func (h *Handler) UpdateProfile(c *gin.Context) {
	claims := middleware.GetUserClaims(c)
	if claims == nil {
		c.JSON(http.StatusUnauthorized, response.Fail(errorcode.InvalidToken))
		return
	}

	var req user.UpdateProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, response.FailWithMessage(err.Error(), http.StatusBadRequest))
		return
	}

	profile, err := h.userSvc.UpdateProfile(claims.UserID, &req)
	if err != nil {
		errMsg := err.Error()
		switch errMsg {
		case errorcode.UserNotFound.Message:
			c.JSON(errorcode.UserNotFound.HTTPStatus, response.Fail(errorcode.UserNotFound))
		case errorcode.DatabaseError.Message:
			c.JSON(errorcode.DatabaseError.HTTPStatus, response.Fail(errorcode.DatabaseError))
		default:
			c.JSON(errorcode.ServerError.HTTPStatus, response.Fail(errorcode.ServerError))
		}
		return
	}

	c.JSON(http.StatusOK, response.Success(map[string]interface{}{
		"profile": profile,
	}))
}
