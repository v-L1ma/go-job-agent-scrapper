package handlers

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"job-scrapper/internal/browser"
	"job-scrapper/internal/config"
	"job-scrapper/internal/database"
	"job-scrapper/internal/orchestrator"
	"job-scrapper/internal/scheduler"
	"job-scrapper/internal/scraper"

	"github.com/labstack/echo/v5"
)

func ActiveScrapper(c *echo.Context) error  {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	cfg := config.Load()

	logger.Info("starting scraper service", "interval_minutes", cfg.IntervalMinutes)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	dbPool, err := database.NewPool(ctx, cfg.PostgresDSN)
	if err != nil {
		logger.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer dbPool.Close()
	logger.Info("connected to database")

	repo := database.NewRepository(dbPool)

	var bm *browser.Manager

	if !cfg.DisableBrowserScrapers {
		bm = browser.NewManager(cfg.Headless, cfg.BrowserBinPath)
		logger.Info("browser manager created (will start per execution cycle)")
	} else {
		logger.Info("browser scrapers disabled via DISABLE_BROWSER_SCRAPERS")
	}

	gupyScraper := scraper.NewGupyScraper(cfg, logger)
	greenhouseScraper := scraper.NewGreenhouseScraper(cfg, logger)

	var vagasScraper *scraper.VagasComBrScraper
	var linkedInScraper *scraper.LinkedInScraper
	if bm != nil {
		vagasScraper = scraper.NewVagasComBrScraper(cfg, bm, logger)
		linkedInScraper = scraper.NewLinkedInScraper(cfg, bm, logger)
	}

	orch := orchestrator.New(
		cfg, repo, logger,
		gupyScraper, greenhouseScraper,
		vagasScraper, linkedInScraper,
	)

	sched := scheduler.New(cfg, orch, bm, logger)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		logger.Info("shutdown signal received")
		cancel()
	}()

	if err := sched.Start(ctx); err != nil && err != context.Canceled {
		logger.Error("scheduler stopped with error", "error", err)
		os.Exit(1)
	}

	logger.Info("shutdown complete")
	return c.JSON(http.StatusOK, map[string]string{
		"message":"Ativando scrapper",
	})
}
