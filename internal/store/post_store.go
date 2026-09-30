package store

import (
	"context"
	"time"

	"gorm.io/gorm"
)

// Post is a blog post of the demo API.
type Post struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	// DeletedAt enables soft delete; see User.DeletedAt.
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
	Title     string         `gorm:"size:255;not null" json:"title"`
	Content   string         `gorm:"type:text" json:"content"`
	Published bool           `gorm:"default:false" json:"published"`
	AuthorID  uint           `gorm:"not null" json:"author_id"`
	Author    *User          `gorm:"foreignKey:AuthorID" json:"author,omitempty"`
}

// PostStore reads and writes posts. List and Get also load the author.
type PostStore interface {
	List(ctx context.Context) ([]Post, error)
	// Get returns ErrNotFound when the post does not exist.
	Get(ctx context.Context, id uint) (*Post, error)
	Create(ctx context.Context, post *Post) error
	Update(ctx context.Context, post *Post) error
	// Delete soft-deletes the post. It returns no error when the post does not exist.
	Delete(ctx context.Context, id uint) error
}
