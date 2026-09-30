package gormstore

import (
	"context"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"AskCore/internal/store"
)

var _ store.PostStore = (*PostStore)(nil)

// PostStore implements store.PostStore with GORM.
type PostStore struct {
	db *gorm.DB
}

// NewPostStore returns a PostStore that uses db.
func NewPostStore(db *gorm.DB) *PostStore {
	return &PostStore{db: db}
}

func (s *PostStore) List(ctx context.Context) ([]store.Post, error) {
	var posts []store.Post
	if err := s.db.WithContext(ctx).Preload("Author").Find(&posts).Error; err != nil {
		return nil, err
	}
	return posts, nil
}

func (s *PostStore) Get(ctx context.Context, id uint) (*store.Post, error) {
	var post store.Post
	if err := s.db.WithContext(ctx).Preload("Author").First(&post, id).Error; err != nil {
		return nil, notFound(err)
	}
	return &post, nil
}

func (s *PostStore) Create(ctx context.Context, post *store.Post) error {
	return s.db.WithContext(ctx).Create(post).Error
}

func (s *PostStore) Update(ctx context.Context, post *store.Post) error {
	// Save only this row. Without Omit, GORM also sends an INSERT ... ON CONFLICT DO NOTHING for each loaded association (for example Post.Author).
	return s.db.WithContext(ctx).Omit(clause.Associations).Save(post).Error
}

func (s *PostStore) Delete(ctx context.Context, id uint) error {
	return s.db.WithContext(ctx).Delete(&store.Post{}, id).Error
}
