package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"job-scrapper/internal/config"
	"job-scrapper/internal/orchestrator"
)

type Scheduler struct {
	cfg          *config.Config
	orchestrator *orchestrator.Orchestrator
	logger       *slog.Logger
	stopCh       chan struct{}
	running      bool
}

func New(cfg *config.Config, orch *orchestrator.Orchestrator, logger *slog.Logger) *Scheduler {
	return &Scheduler{
		cfg:          cfg,
		orchestrator: orch,
		logger:       logger.With("component", "scheduler"),
		stopCh:       make(chan struct{}),
	}
}

func (s *Scheduler) Start(ctx context.Context) error {
	s.running = true

	s.logger.Info("scheduler started", "interval_minutes", s.cfg.IntervalMinutes)

	if err := s.runOnce(ctx); err != nil {
		s.logger.Error("initial run failed", "error", err)
	}

	ticker := time.NewTicker(time.Duration(s.cfg.IntervalMinutes) * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.running = false
			return ctx.Err()

		case <-ticker.C:
			if err := s.runOnce(ctx); err != nil {
				s.logger.Error("scheduled run failed", "error", err)
			}

		case <-s.stopCh:
			s.running = false
			return nil
		}
	}
}

func (s *Scheduler) runOnce(ctx context.Context) error {
	if s.orchestrator.IsRunning() {
		s.logger.Warn("previous execution still in progress, skipping")
		return nil
	}

	start := time.Now()
	s.logger.Info("starting scheduled execution")

	if err := s.orchestrator.Execute(ctx); err != nil {
		return fmt.Errorf("execute: %w", err)
	}

	s.logger.Info("scheduled execution complete", "duration", time.Since(start))
	return nil
}

func (s *Scheduler) Stop() {
	close(s.stopCh)
}

func (s *Scheduler) IsRunning() bool {
	return s.running
}
