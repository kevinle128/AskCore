package http

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"AskCore/internal/store"
)

// UserHandler serves /api/users. It uses the store.UserStore interface only.
type UserHandler struct {
	users store.UserStore
}

// NewUserHandler returns a UserHandler.
func NewUserHandler(users store.UserStore) *UserHandler {
	return &UserHandler{users: users}
}

// Register adds the routes of this handler to e.
func (h *UserHandler) Register(e *echo.Echo) {
	e.GET("/api/users", h.list)
	e.POST("/api/users", h.create)
	e.GET("/api/users/:id", h.get)
	e.PUT("/api/users/:id", h.update)
	e.DELETE("/api/users/:id", h.delete)
}

// userCreateRequest is the body of POST /api/users.
type userCreateRequest struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

// userUpdateRequest is the body of PUT /api/users/:id. A nil field is not changed.
type userUpdateRequest struct {
	Name  *string `json:"name,omitempty"`
	Email *string `json:"email,omitempty"`
}

func (h *UserHandler) list(c echo.Context) error {
	users, err := h.users.List(c.Request().Context())
	if err != nil {
		return errorJSON(c, http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, users)
}

func (h *UserHandler) get(c echo.Context) error {
	id, ok := idParam(c)
	if !ok {
		return errorJSON(c, http.StatusBadRequest, "Invalid ID")
	}
	user, err := h.users.Get(c.Request().Context(), id)
	if err != nil {
		return storeError(c, err, "User not found")
	}
	return c.JSON(http.StatusOK, user)
}

func (h *UserHandler) create(c echo.Context) error {
	var input userCreateRequest
	if err := c.Bind(&input); err != nil {
		return errorJSON(c, http.StatusBadRequest, err.Error())
	}
	user := store.User{Name: input.Name, Email: input.Email}
	if err := h.users.Create(c.Request().Context(), &user); err != nil {
		return errorJSON(c, http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusCreated, user)
}

func (h *UserHandler) update(c echo.Context) error {
	id, ok := idParam(c)
	if !ok {
		return errorJSON(c, http.StatusBadRequest, "Invalid ID")
	}
	ctx := c.Request().Context()
	user, err := h.users.Get(ctx, id)
	if err != nil {
		return storeError(c, err, "User not found")
	}

	var input userUpdateRequest
	if err := c.Bind(&input); err != nil {
		return errorJSON(c, http.StatusBadRequest, err.Error())
	}
	if input.Name != nil {
		user.Name = *input.Name
	}
	if input.Email != nil {
		user.Email = *input.Email
	}

	if err := h.users.Update(ctx, user); err != nil {
		return errorJSON(c, http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, user)
}

func (h *UserHandler) delete(c echo.Context) error {
	id, ok := idParam(c)
	if !ok {
		return errorJSON(c, http.StatusBadRequest, "Invalid ID")
	}
	if err := h.users.Delete(c.Request().Context(), id); err != nil {
		return errorJSON(c, http.StatusInternalServerError, err.Error())
	}
	return c.NoContent(http.StatusNoContent)
}
