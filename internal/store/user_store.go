package store

import (
	"context"
	"time"

	"gorm.io/gorm"
)

// User is a user of the demo API.
type User struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	// DeletedAt enables soft delete. It keeps json:"-" because GORM filters
	// soft-deleted rows, so a returned User never has a value here.
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
	Name      string         `gorm:"size:255;not null" json:"name"`
	Email     string         `gorm:"size:255;uniqueIndex;not null" json:"email"`
	Posts     []Post         `gorm:"foreignKey:AuthorID" json:"posts,omitempty"`
}

// UserStore reads and writes users.
type UserStore interface {
	List(ctx context.Context) ([]User, error)
	// Get returns ErrNotFound when the user does not exist.
	Get(ctx context.Context, id uint) (*User, error)
	Create(ctx context.Context, user *User) error
	Update(ctx context.Context, user *User) error
	// Delete soft-deletes the user. It returns no error when the user does not exist.
	Delete(ctx context.Context, id uint) error
}
