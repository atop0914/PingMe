package auth

import (
	"net/http"

	"PingMe/internal/errorcode"
	"PingMe/internal/model/user"
	"PingMe/internal/service"
	"PingMe/pkg/response"

	"github.com/gin-gonic/gin"
)

// Handler handles authentication requests
type Handler struct {
	userSvc *service.Service
}

// NewHandler creates a new auth handler
func NewHandler(userSvc *service.Service) *Handler {
	return &Handler{
		userSvc: userSvc,
	}
}

// Register handles user registration
// @Summary User registration
// @Description Register a new user
// @Tags auth
// @Accept json
// @Produce json
// @Param request body user.RegisterRequest true "Registration details"
// @Success 200 {object} response.Response
// @Router /auth/register [post]
func (h *Handler) Register(c *gin.Context) {
	var req user.RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, response.FailWithMessage(err.Error(), http.StatusBadRequest))
		return
	}

	u, err := h.userSvc.Register(&req)
	if err != nil {
		errMsg := err.Error()
		switch errMsg {
		case errorcode.UserAlreadyExists.Message:
			c.JSON(errorcode.UserAlreadyExists.HTTPStatus, response.Fail(errorcode.UserAlreadyExists))
		case errorcode.DatabaseError.Message:
			c.JSON(errorcode.DatabaseError.HTTPStatus, response.Fail(errorcode.DatabaseError))
		default:
			c.JSON(errorcode.ServerError.HTTPStatus, response.Fail(errorcode.ServerError))
		}
		return
	}

	c.JSON(http.StatusCreated, response.Success(map[string]interface{}{
		"user": u.ToProfile(),
	}))
}

// Login handles user login
// @Summary User login
// @Description Authenticate user and get JWT token
// @Tags auth
// @Accept json
// @Produce json
// @Param request body user.LoginRequest true "Login credentials"
// @Success 200 {object} response.Response
// @Router /auth/login [post]
func (h *Handler) Login(c *gin.Context) {
	var req user.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, response.FailWithMessage(err.Error(), http.StatusBadRequest))
		return
	}

	resp, err := h.userSvc.Login(&req)
	if err != nil {
		errMsg := err.Error()
		switch errMsg {
		case errorcode.UserNotFound.Message:
			c.JSON(errorcode.UserNotFound.HTTPStatus, response.Fail(errorcode.UserNotFound))
		case errorcode.InvalidPassword.Message:
			c.JSON(errorcode.InvalidPassword.HTTPStatus, response.Fail(errorcode.InvalidPassword))
		case errorcode.ServerError.Message:
			c.JSON(errorcode.ServerError.HTTPStatus, response.Fail(errorcode.ServerError))
		default:
			c.JSON(errorcode.ServerError.HTTPStatus, response.Fail(errorcode.ServerError))
		}
		return
	}

	c.JSON(http.StatusOK, response.Success(map[string]interface{}{
		"token":     resp.Token,
		"expires_in": resp.ExpiresIn,
		"user":      resp.User,
	}))
}
