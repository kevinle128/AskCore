package http

import (
	"strconv"

	"github.com/labstack/echo/v4"
)

// idParam parses the ":id" path parameter.
func idParam(c echo.Context) (uint, bool) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		return 0, false
	}
	return uint(id), true
}
