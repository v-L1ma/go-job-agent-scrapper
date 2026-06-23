package routes

import (
	"job-scrapper/internal/handler"
	"net/http"

	"github.com/labstack/echo/v5"
)

func RegisterRoutes(e *echo.Echo) {
	e.GET("/scrappe", handlers.ActiveScrapper)

	e.GET("/home", func(c *echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"message": "Hello, World!"})
	})
}