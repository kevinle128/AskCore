package gormstore

import (
	"gorm.io/gorm"

	"AskCore/internal/store"
)

// NewStores builds all GORM stores on db.
func NewStores(db *gorm.DB) *store.Stores {
	return &store.Stores{
		Users: NewUserStore(db),
		Posts: NewPostStore(db),
	}
}
