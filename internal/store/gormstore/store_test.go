package gormstore

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"AskCore/internal/migrations"
	"AskCore/internal/store"
)

// newTestDB returns a migrated SQLite database in a temporary folder.
func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "test.db")
	require.NoError(t, migrations.Up(dsn))
	db, err := Open(dsn)
	require.NoError(t, err)
	t.Cleanup(func() {
		sqlDB, err := db.DB()
		require.NoError(t, err)
		require.NoError(t, sqlDB.Close())
	})
	return db
}

func TestUserStore_CRUDAndSoftDelete(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	users := NewUserStore(db)

	user := &store.User{Name: "Ann", Email: "ann@example.com"}
	require.NoError(t, users.Create(ctx, user))
	require.NotZero(t, user.ID)

	got, err := users.Get(ctx, user.ID)
	require.NoError(t, err)
	require.Equal(t, "Ann", got.Name)

	got.Name = "Ann B"
	require.NoError(t, users.Update(ctx, got))
	got, err = users.Get(ctx, user.ID)
	require.NoError(t, err)
	require.Equal(t, "Ann B", got.Name)

	list, err := users.List(ctx)
	require.NoError(t, err)
	require.Len(t, list, 1)

	require.NoError(t, users.Delete(ctx, user.ID))
	_, err = users.Get(ctx, user.ID)
	require.ErrorIs(t, err, store.ErrNotFound)

	// The row stays in the table with deleted_at set.
	var deletedCount int64
	require.NoError(t, db.Unscoped().Model(&store.User{}).Where("id = ? AND deleted_at IS NOT NULL", user.ID).Count(&deletedCount).Error)
	require.EqualValues(t, 1, deletedCount)
}

func TestUserStore_GetUnknownID(t *testing.T) {
	_, err := NewUserStore(newTestDB(t)).Get(context.Background(), 42)
	require.ErrorIs(t, err, store.ErrNotFound)
}

func TestPostStore_LoadsAuthor(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	stores := NewStores(db)

	author := &store.User{Name: "Ann", Email: "ann@example.com"}
	require.NoError(t, stores.Users.Create(ctx, author))
	post := &store.Post{Title: "Hello", Content: "First post", AuthorID: author.ID}
	require.NoError(t, stores.Posts.Create(ctx, post))

	got, err := stores.Posts.Get(ctx, post.ID)
	require.NoError(t, err)
	require.NotNil(t, got.Author)
	require.Equal(t, "Ann", got.Author.Name)

	list, err := stores.Posts.List(ctx)
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.NotNil(t, list[0].Author)
}

func TestPostStore_UpdateDoesNotWriteAuthor(t *testing.T) {
	ctx := context.Background()
	stores := NewStores(newTestDB(t))

	author := &store.User{Name: "Ann", Email: "ann@example.com"}
	require.NoError(t, stores.Users.Create(ctx, author))
	post := &store.Post{Title: "Hello", AuthorID: author.ID}
	require.NoError(t, stores.Posts.Create(ctx, post))

	got, err := stores.Posts.Get(ctx, post.ID)
	require.NoError(t, err)
	got.Title = "Hello again"
	got.Author.Name = "changed through the post"
	require.NoError(t, stores.Posts.Update(ctx, got))

	reloadedAuthor, err := stores.Users.Get(ctx, author.ID)
	require.NoError(t, err)
	require.Equal(t, "Ann", reloadedAuthor.Name)

	reloadedPost, err := stores.Posts.Get(ctx, post.ID)
	require.NoError(t, err)
	require.Equal(t, "Hello again", reloadedPost.Title)
}
