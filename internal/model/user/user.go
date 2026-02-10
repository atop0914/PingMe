package user

import (
	"time"

	"github.com/google/uuid"
)

// User represents a user in the system
type User struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	UserID    string    `json:"user_id" gorm:"type:varchar(36);uniqueIndex;not null"`
	Username  string    `json:"username" gorm:"type:varchar(50);uniqueIndex;not null"`
	Email     string    `json:"email" gorm:"type:varchar(255);uniqueIndex"`
	Password  string    `json:"-" gorm:"type:varchar(255);not null"`
	Nickname  string    `json:"nickname" gorm:"type:varchar(100)"`
	AvatarURL string    `json:"avatar_url" gorm:"type:varchar(500)"`
	Status    int       `json:"status" gorm:"default:1"` // 1: active, 0: inactive
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// BeforeCreate generates a UUID for the user before creation
func (u *User) BeforeCreate() error {
	if u.UserID == "" {
		u.UserID = uuid.New().String()
	}
	return nil
}

// UserProfile represents the public profile of a user
type UserProfile struct {
	UserID    string `json:"user_id"`
	Username  string `json:"username"`
	Nickname  string `json:"nickname"`
	AvatarURL string `json:"avatar_url"`
	Status    int    `json:"status"`
	CreatedAt string `json:"created_at"`
}

// ToProfile converts a User to UserProfile
func (u *User) ToProfile() UserProfile {
	return UserProfile{
		UserID:    u.UserID,
		Username:  u.Username,
		Nickname:  u.Nickname,
		AvatarURL: u.AvatarURL,
		Status:    u.Status,
		CreatedAt: u.CreatedAt.Format(time.RFC3339),
	}
}

// RegisterRequest represents a user registration request
type RegisterRequest struct {
	Username string `json:"username" binding:"required,min=3,max=50"`
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=6,max=100"`
	Nickname string `json:"nickname" binding:"max=100"`
}

// LoginRequest represents a user login request
type LoginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// UpdateProfileRequest represents a profile update request
type UpdateProfileRequest struct {
	Nickname  string `json:"nickname" binding:"max=100"`
	AvatarURL string `json:"avatar_url" binding:"max=500"`
}

// LoginResponse represents a successful login response
type LoginResponse struct {
	Token    string       `json:"token"`
	ExpiresIn int         `json:"expires_in"`
	User     UserProfile  `json:"user"`
}
