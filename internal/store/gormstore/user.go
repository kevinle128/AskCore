package gormstore

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"AskCore/internal/store"
)

var _ store.UserStore = (*UserStore)(nil)

// UserStore implements store.UserStore with GORM.
type UserStore struct {
	db *gorm.DB
}

// NewUserStore returns a UserStore that uses db.
func NewUserStore(db *gorm.DB) *UserStore {
	return &UserStore{db: db}
}

func (s *UserStore) List(ctx context.Context) ([]store.User, error) {
	var users []store.User
	if err := s.db.WithContext(ctx).Find(&users).Error; err != nil {
		return nil, err
	}
	return users, nil
}

func (s *UserStore) Get(ctx context.Context, id uint) (*store.User, error) {
	var user store.User
	if err := s.db.WithContext(ctx).First(&user, id).Error; err != nil {
		return nil, notFound(err)
	}
	return &user, nil
}

func (s *UserStore) Create(ctx context.Context, user *store.User) error {
	return s.db.WithContext(ctx).Create(user).Error
}

func (s *UserStore) Update(ctx context.Context, user *store.User) error {
	// Save only this row. Without Omit, GORM also sends an INSERT ... ON CONFLICT DO NOTHING for each loaded association (for example Post.Author).
	return s.db.WithContext(ctx).Omit(clause.Associations).Save(user).Error
}

func (s *UserStore) Delete(ctx context.Context, id uint) error {
	return s.db.WithContext(ctx).Delete(&store.User{}, id).Error
}

// notFound maps the GORM not-found error to store.ErrNotFound.
func notFound(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return store.ErrNotFound
	}
	return err
}
