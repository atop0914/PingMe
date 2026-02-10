package user

import (
	"PingMe/internal/model/user"

	"gorm.io/gorm"
)

// Repository handles user data access
type Repository struct {
	db *gorm.DB
}

// NewRepository creates a new user repository
func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// Create creates a new user
func (r *Repository) Create(u *user.User) error {
	return r.db.Create(u).Error
}

// GetByUserID retrieves a user by user_id
func (r *Repository) GetByUserID(userID string) (*user.User, error) {
	var u user.User
	err := r.db.Where("user_id = ?", userID).First(&u).Error
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// GetByUsername retrieves a user by username
func (r *Repository) GetByUsername(username string) (*user.User, error) {
	var u user.User
	err := r.db.Where("username = ?", username).First(&u).Error
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// GetByEmail retrieves a user by email
func (r *Repository) GetByEmail(email string) (*user.User, error) {
	var u user.User
	err := r.db.Where("email = ?", email).First(&u).Error
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// ExistsByUsername checks if a username exists
func (r *Repository) ExistsByUsername(username string) (bool, error) {
	var count int64
	err := r.db.Model(&user.User{}).Where("username = ?", username).Count(&count).Error
	return count > 0, err
}

// ExistsByEmail checks if an email exists
func (r *Repository) ExistsByEmail(email string) (bool, error) {
	var count int64
	err := r.db.Model(&user.User{}).Where("email = ?", email).Count(&count).Error
	return count > 0, err
}

// Update updates a user
func (r *Repository) Update(u *user.User) error {
	return r.db.Save(u).Error
}

// Delete deletes a user by user_id
func (r *Repository) Delete(userID string) error {
	return r.db.Where("user_id = ?", userID).Delete(&user.User{}).Error
}

// InitSchema creates the user table if it doesn't exist
func (r *Repository) InitSchema() error {
	return r.db.AutoMigrate(&user.User{})
}
