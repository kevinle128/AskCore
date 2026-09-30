package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"

	"AskCore/internal/store"
)

// fakeUserStore is an in-memory store.UserStore for handler tests.
type fakeUserStore struct {
	users  map[uint]store.User
	nextID uint
	err    error // returned by every method when set
}

func newFakeUserStore() *fakeUserStore {
	return &fakeUserStore{users: map[uint]store.User{}, nextID: 1}
}

func (f *fakeUserStore) List(context.Context) ([]store.User, error) {
	if f.err != nil {
		return nil, f.err
	}
	out := make([]store.User, 0, len(f.users))
	for _, u := range f.users {
		out = append(out, u)
	}
	return out, nil
}

func (f *fakeUserStore) Get(_ context.Context, id uint) (*store.User, error) {
	if f.err != nil {
		return nil, f.err
	}
	u, ok := f.users[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return &u, nil
}

func (f *fakeUserStore) Create(_ context.Context, u *store.User) error {
	if f.err != nil {
		return f.err
	}
	u.ID = f.nextID
	f.nextID++
	f.users[u.ID] = *u
	return nil
}

func (f *fakeUserStore) Update(_ context.Context, u *store.User) error {
	if f.err != nil {
		return f.err
	}
	f.users[u.ID] = *u
	return nil
}

func (f *fakeUserStore) Delete(_ context.Context, id uint) error {
	if f.err != nil {
		return f.err
	}
	delete(f.users, id)
	return nil
}

func serve(t *testing.T, h interface{ Register(*echo.Echo) }, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	e := echo.New()
	h.Register(e)
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func TestUserHandler_CreateGetUpdateDelete(t *testing.T) {
	users := newFakeUserStore()
	h := NewUserHandler(users)

	rec := serve(t, h, http.MethodPost, "/api/users", `{"name":"Ann","email":"ann@example.com"}`)
	require.Equal(t, http.StatusCreated, rec.Code)
	var created store.User
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))
	require.Equal(t, uint(1), created.ID)
	require.NotContains(t, rec.Body.String(), "deleted_at")

	rec = serve(t, h, http.MethodGet, "/api/users/1", "")
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"name":"Ann"`)

	rec = serve(t, h, http.MethodPut, "/api/users/1", `{"name":"Ann B"}`)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "Ann B", users.users[1].Name)
	require.Equal(t, "ann@example.com", users.users[1].Email, "a field that is not sent stays the same")

	rec = serve(t, h, http.MethodDelete, "/api/users/1", "")
	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Empty(t, users.users)
}

func TestUserHandler_Errors(t *testing.T) {
	users := newFakeUserStore()
	h := NewUserHandler(users)

	rec := serve(t, h, http.MethodGet, "/api/users/abc", "")
	require.Equal(t, http.StatusBadRequest, rec.Code)

	rec = serve(t, h, http.MethodGet, "/api/users/99", "")
	require.Equal(t, http.StatusNotFound, rec.Code)
	require.JSONEq(t, `{"error":"User not found"}`, rec.Body.String())

	rec = serve(t, h, http.MethodPut, "/api/users/99", `{"name":"x"}`)
	require.Equal(t, http.StatusNotFound, rec.Code)

	users.err = errors.New("db down")
	rec = serve(t, h, http.MethodGet, "/api/users/1", "")
	require.Equal(t, http.StatusInternalServerError, rec.Code, "a store failure is not reported as 404")
}

func TestHealthHandler(t *testing.T) {
	rec := serve(t, NewHealthHandler(), http.MethodGet, "/health", "")
	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{"status":"ok","message":"Server is running"}`, rec.Body.String())
}
