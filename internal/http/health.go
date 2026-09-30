package http

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

// HealthHandler serves the health check and the root endpoint.
type HealthHandler struct{}

// NewHealthHandler returns a HealthHandler.
func NewHealthHandler() *HealthHandler {
	return &HealthHandler{}
}

// Register adds the routes of this handler to e.
func (h *HealthHandler) Register(e *echo.Echo) {
	e.GET("/health", h.health)
	e.GET("/", h.root)
}

func (h *HealthHandler) health(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]string{
		"status":  "ok",
		"message": "Server is running",
	})
}

func (h *HealthHandler) root(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]string{
		"message": "Welcome to AskCore!",
	})
}
