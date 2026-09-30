package http

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"AskCore/internal/store"
)

// PostHandler serves /api/posts. It uses the store.PostStore interface only.
type PostHandler struct {
	posts store.PostStore
}

// NewPostHandler returns a PostHandler.
func NewPostHandler(posts store.PostStore) *PostHandler {
	return &PostHandler{posts: posts}
}

// Register adds the routes of this handler to e.
func (h *PostHandler) Register(e *echo.Echo) {
	e.GET("/api/posts", h.list)
	e.POST("/api/posts", h.create)
	e.GET("/api/posts/:id", h.get)
	e.PUT("/api/posts/:id", h.update)
	e.DELETE("/api/posts/:id", h.delete)
}

// postCreateRequest is the body of POST /api/posts.
type postCreateRequest struct {
	Title    string `json:"title"`
	Content  string `json:"content"`
	AuthorID uint   `json:"author_id"`
}

// postUpdateRequest is the body of PUT /api/posts/:id. A nil field is not changed.
type postUpdateRequest struct {
	Title     *string `json:"title,omitempty"`
	Content   *string `json:"content,omitempty"`
	Published *bool   `json:"published,omitempty"`
}

func (h *PostHandler) list(c echo.Context) error {
	posts, err := h.posts.List(c.Request().Context())
	if err != nil {
		return errorJSON(c, http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, posts)
}

func (h *PostHandler) get(c echo.Context) error {
	id, ok := idParam(c)
	if !ok {
		return errorJSON(c, http.StatusBadRequest, "Invalid ID")
	}
	post, err := h.posts.Get(c.Request().Context(), id)
	if err != nil {
		return storeError(c, err, "Post not found")
	}
	return c.JSON(http.StatusOK, post)
}

func (h *PostHandler) create(c echo.Context) error {
	var input postCreateRequest
	if err := c.Bind(&input); err != nil {
		return errorJSON(c, http.StatusBadRequest, err.Error())
	}
	post := store.Post{Title: input.Title, Content: input.Content, AuthorID: input.AuthorID}
	if err := h.posts.Create(c.Request().Context(), &post); err != nil {
		return errorJSON(c, http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusCreated, post)
}

func (h *PostHandler) update(c echo.Context) error {
	id, ok := idParam(c)
	if !ok {
		return errorJSON(c, http.StatusBadRequest, "Invalid ID")
	}
	ctx := c.Request().Context()
	post, err := h.posts.Get(ctx, id)
	if err != nil {
		return storeError(c, err, "Post not found")
	}

	var input postUpdateRequest
	if err := c.Bind(&input); err != nil {
		return errorJSON(c, http.StatusBadRequest, err.Error())
	}
	if input.Title != nil {
		post.Title = *input.Title
	}
	if input.Content != nil {
		post.Content = *input.Content
	}
	if input.Published != nil {
		post.Published = *input.Published
	}

	if err := h.posts.Update(ctx, post); err != nil {
		return errorJSON(c, http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, post)
}

func (h *PostHandler) delete(c echo.Context) error {
	id, ok := idParam(c)
	if !ok {
		return errorJSON(c, http.StatusBadRequest, "Invalid ID")
	}
	if err := h.posts.Delete(c.Request().Context(), id); err != nil {
		return errorJSON(c, http.StatusInternalServerError, err.Error())
	}
	return c.NoContent(http.StatusNoContent)
}
