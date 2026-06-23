package routes

import (
	"job-scrapper/internal/handler"
	"net/http"

	"github.com/labstack/echo/v5"
)

func RegisterRoutes(e *echo.Echo) {
	e.GET("/scrapper", handlers.ActiveScrapper)

	e.GET("/", func(c *echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"message": "Server is running"})
	})
}