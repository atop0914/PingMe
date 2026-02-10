package service

import (
	"errors"

	"PingMe/internal/errorcode"
	"PingMe/internal/model/user"
	"PingMe/internal/pkg/jwt"
	"PingMe/internal/pkg/password"
	userrepo "PingMe/internal/repository/user"
)

// Service handles user business logic
type Service struct {
	repo    *userrepo.Repository
	jwtSvc  *jwt.TokenService
}

// NewService creates a new user service
func NewService(repo *userrepo.Repository, jwtSvc *jwt.TokenService) *Service {
	return &Service{
		repo:   repo,
		jwtSvc: jwtSvc,
	}
}

// Register registers a new user
func (s *Service) Register(req *user.RegisterRequest) (*user.User, error) {
	// Check if username exists
	exists, err := s.repo.ExistsByUsername(req.Username)
	if err != nil {
		return nil, errors.New(errorcode.DatabaseError.Message)
	}
	if exists {
		return nil, errors.New(errorcode.UserAlreadyExists.Message)
	}

	// Check if email exists
	exists, err = s.repo.ExistsByEmail(req.Email)
	if err != nil {
		return nil, errors.New(errorcode.DatabaseError.Message)
	}
	if exists {
		return nil, errors.New(errorcode.UserAlreadyExists.Message)
	}

	// Hash password
	hashedPassword, err := password.Hash(req.Password)
	if err != nil {
		return nil, errors.New(errorcode.ServerError.Message)
	}

	// Create user
	u := &user.User{
		Username: req.Username,
		Email:    req.Email,
		Password: hashedPassword,
		Nickname: req.Nickname,
	}

	if err := u.BeforeCreate(); err != nil {
		return nil, errors.New(errorcode.ServerError.Message)
	}

	if err := s.repo.Create(u); err != nil {
		return nil, errors.New(errorcode.DatabaseError.Message)
	}

	return u, nil
}

// Login authenticates a user and returns a JWT token
func (s *Service) Login(req *user.LoginRequest) (*user.LoginResponse, error) {
	// Find user by username
	u, err := s.repo.GetByUsername(req.Username)
	if err != nil {
		return nil, errors.New(errorcode.UserNotFound.Message)
	}

	// Verify password
	if !password.Compare(req.Password, u.Password) {
		return nil, errors.New(errorcode.InvalidPassword.Message)
	}

	// Generate JWT token
	token, err := s.jwtSvc.GenerateToken(u.UserID, u.Username)
	if err != nil {
		return nil, errors.New(errorcode.ServerError.Message)
	}

	return &user.LoginResponse{
		Token:     token,
		ExpiresIn: int(s.jwtSvc.GetExpirationDuration().Seconds()),
		User:      u.ToProfile(),
	}, nil
}

// GetProfile retrieves a user's profile
func (s *Service) GetProfile(userID string) (*user.UserProfile, error) {
	u, err := s.repo.GetByUserID(userID)
	if err != nil {
		return nil, errors.New(errorcode.UserNotFound.Message)
	}

	profile := u.ToProfile()
	return &profile, nil
}

// UpdateProfile updates a user's profile
func (s *Service) UpdateProfile(userID string, req *user.UpdateProfileRequest) (*user.UserProfile, error) {
	u, err := s.repo.GetByUserID(userID)
	if err != nil {
		return nil, errors.New(errorcode.UserNotFound.Message)
	}

	// Update fields if provided
	if req.Nickname != "" {
		u.Nickname = req.Nickname
	}
	if req.AvatarURL != "" {
		u.AvatarURL = req.AvatarURL
	}

	if err := s.repo.Update(u); err != nil {
		return nil, errors.New(errorcode.DatabaseError.Message)
	}

	profile := u.ToProfile()
	return &profile, nil
}

// GetUserByID retrieves a user by user ID (for internal use)
func (s *Service) GetUserByID(userID string) (*user.User, error) {
	return s.repo.GetByUserID(userID)
}
