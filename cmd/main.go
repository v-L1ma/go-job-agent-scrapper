package main

import (
	"os"
	"runtime/debug"

	"job-scrapper/internal/routes"

	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
)

func getPort() string {
	if p := os.Getenv("PORT"); p != "" {
		return ":" + p
	}
	return ":8080"
}

func main() {
  debug.SetMemoryLimit(400 * 1024 * 1024)

  e := echo.New()

  e.Use(middleware.RequestLogger())
  e.Use(middleware.Recover())

  routes.RegisterRoutes(e)

  addr := getPort()
  e.Logger.Info("starting server", "addr", addr)

  if err := e.Start(addr); err != nil {
    e.Logger.Error("failed to start server", "error", err)
  }
}