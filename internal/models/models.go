package models

import (
	"time"

	"gorm.io/gorm"
)

// User represents a user in the system
type User struct {
	ID        uint           `gorm:"primarykey" json:"id"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
	Name      string         `gorm:"size:255;not null" json:"name"`
	Email     string         `gorm:"size:255;uniqueIndex;not null" json:"email"`
	Posts     []Post         `gorm:"foreignKey:AuthorID" json:"posts,omitempty"`
}

// Post represents a blog post
type Post struct {
	ID        uint           `gorm:"primarykey" json:"id"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
	Title     string         `gorm:"size:255;not null" json:"title"`
	Content   string         `gorm:"type:text" json:"content"`
	Published bool           `gorm:"default:false" json:"published"`
	AuthorID  uint           `gorm:"not null" json:"author_id"`
	Author    *User          `gorm:"foreignKey:AuthorID" json:"author,omitempty"`
}

// UserCreate is used for creating a new user
type UserCreate struct {
	Name  string `json:"name" binding:"required"`
	Email string `json:"email" binding:"required,email"`
}

// UserUpdate is used for updating a user
type UserUpdate struct {
	Name  *string `json:"name,omitempty"`
	Email *string `json:"email,omitempty"`
}

// PostCreate is used for creating a new post
type PostCreate struct {
	Title    string `json:"title" binding:"required"`
	Content  string `json:"content"`
	AuthorID uint   `json:"author_id" binding:"required"`
}

// PostUpdate is used for updating a post
type PostUpdate struct {
	Title     *string `json:"title,omitempty"`
	Content   *string `json:"content,omitempty"`
	Published *bool   `json:"published,omitempty"`
}
