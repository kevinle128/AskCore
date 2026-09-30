package http

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v4"

	"AskCore/internal/store"
)

// errorJSON writes {"error": message} with the given status.
func errorJSON(c echo.Context, status int, message string) error {
	return c.JSON(status, map[string]string{"error": message})
}

// storeError maps a store error to a response: ErrNotFound gives 404 with
// notFoundMessage, any other error gives 500.
func storeError(c echo.Context, err error, notFoundMessage string) error {
	if errors.Is(err, store.ErrNotFound) {
		return errorJSON(c, http.StatusNotFound, notFoundMessage)
	}
	return errorJSON(c, http.StatusInternalServerError, err.Error())
}
