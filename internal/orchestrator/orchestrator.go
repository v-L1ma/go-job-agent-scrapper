package orchestrator

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"job-scrapper/internal/config"
	"job-scrapper/internal/database"
	"job-scrapper/internal/models"
	"job-scrapper/internal/scraper"
)

type Orchestrator struct {
	scrapers   map[models.Platform]scraper.Scraper
	repo       *database.Repository
	cfg        *config.Config
	logger     *slog.Logger
	running    bool
	mu         sync.Mutex
	lastRun    time.Time
}

func New(
	cfg *config.Config,
	repo *database.Repository,
	logger *slog.Logger,
	gupy scraper.Scraper,
	greenhouse scraper.Scraper,
	vagasComBr scraper.Scraper,
	linkedIn scraper.Scraper,
) *Orchestrator {
	return &Orchestrator{
		scrapers: map[models.Platform]scraper.Scraper{
			models.PlatformGupy:       gupy,
			models.PlatformGreenhouse: greenhouse,
			models.PlatformVagasComBr: vagasComBr,
			models.PlatformLinkedIn:   linkedIn,
		},
		repo:   repo,
		cfg:    cfg,
		logger: logger.With("component", "orchestrator"),
	}
}

func (o *Orchestrator) Execute(ctx context.Context) error {
	o.mu.Lock()
	if o.running {
		o.mu.Unlock()
		return fmt.Errorf("orchestrator already running")
	}
	o.running = true
	o.mu.Unlock()

	defer func() {
		o.mu.Lock()
		o.running = false
		o.lastRun = time.Now()
		o.mu.Unlock()
	}()

	o.logger.Info("starting orchestration")

	contexts, err := o.repo.LoadQueryContexts(ctx)
	if err != nil {
		return fmt.Errorf("load query contexts: %w", err)
	}

	if len(contexts) == 0 {
		o.logger.Info("no active queries found")
		return nil
	}

	userIDs := make([]string, 0, len(contexts))
	userSet := make(map[string]bool)
	for _, c := range contexts {
		if !userSet[c.UserID] {
			userIDs = append(userIDs, c.UserID)
			userSet[c.UserID] = true
		}
	}

	userStates, err := o.repo.LoadUserDailyStates(ctx, userIDs)
	if err != nil {
		return fmt.Errorf("load user daily states: %w", err)
	}

	platformOrder := []models.Platform{
		models.PlatformGupy,
		models.PlatformVagasComBr,
		models.PlatformGreenhouse,
		models.PlatformLinkedIn,
	}

	totalJobs := 0
	allErrors := make([]string, 0)
	queryErrors := make(map[string][]string)

	for _, queryCtx := range contexts {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		o.logger.Info("executing query",
			"query_id", queryCtx.QueryID,
			"query", queryCtx.Keywords,
			"user_id", queryCtx.UserID,
		)

		counters := models.NewQueryExecutionCounters(queryCtx.QueryID)
		userState := userStates[queryCtx.UserID]

		keywords := queryCtx.Keywords
		if len(keywords) == 0 {
			keywords = []string{queryCtx.Query}
		}

		for _, kw := range keywords {
			for _, platform := range platformOrder {
				scraperImpl, ok := o.scrapers[platform]
				if !ok {
					o.logger.Warn("scraper not found in map", "platform", platform)
					continue
				}

				counters.GlobalMaxPerQuery = o.maxPerQueryForPlatform(platform)
				platformMax := o.maxPerQueryForPlatform(platform)

				o.logger.Info("starting platform",
					"platform", platform,
					"keyword", kw,
					"query_id", queryCtx.QueryID,
					"user_id", queryCtx.UserID,
					"max_jobs", platformMax,
					"location", queryCtx.Location,
				)

				req := models.ScrapeRequest{
					Query:    queryCtx.Query,
					Keywords: []string{kw},
					Location: queryCtx.Location,
					Platforms: []string{string(platform)},
				}

				if platform == models.PlatformLinkedIn {
					req.LiAtCookie = o.cfg.LiAtCookie
					req.EasyApplyOnly = o.cfg.EasyApplyOnly

					if o.cfg.LiAtCookie == "" {
						o.logger.Warn("LI_AT_COOKIE not set, LinkedIn may return zero jobs",
							"platform", platform,
							"keyword", kw,
						)
					}
				}

				platformJobCount := 0

				err := o.runWithRetry(ctx, scraperImpl, req, func(job models.ScrapedJob) bool {
					saved, saveErr := o.repo.SaveJob(ctx, &job, queryCtx.QueryID, userState, counters, queryCtx.ExcludeKeywords)
					if saveErr != nil {
						o.logger.Error("save job", "error", saveErr)
						return true
					}
					if saved {
						totalJobs++
						platformJobCount++
					}
					return platformJobCount < platformMax
				})

				o.logger.Info("platform finished",
					"platform", platform,
					"keyword", kw,
					"query_id", queryCtx.QueryID,
					"jobs_collected", platformJobCount,
					"has_error", err != nil,
				)

				if err != nil {
					errMsg := fmt.Sprintf("[%s] key %s: %v", platform, kw, err)
					allErrors = append(allErrors, errMsg)
					queryErrors[string(platform)] = append(queryErrors[string(platform)], errMsg)
					o.logger.Error("scraper failed", "platform", platform, "keyword", kw, "error", err)
				}
			}
		}

		if err := o.repo.UpdateUserSearchQueryLimits(ctx, queryCtx.QueryID, counters.GetPlatformCounters()); err != nil {
			o.logger.Error("update query limits", "query_id", queryCtx.QueryID, "error", err)
		}
	}

	o.logger.Info("orchestration complete",
		"total_jobs", totalJobs,
		"errors", len(allErrors),
	)

	return nil
}

func (o *Orchestrator) maxPerQueryForPlatform(platform models.Platform) int {
	switch platform {
	case models.PlatformGupy:
		return o.cfg.MaxGupyJobsPerQuery
	case models.PlatformGreenhouse:
		return o.cfg.MaxGreenhouseJobsPerQuery
	case models.PlatformVagasComBr:
		return o.cfg.MaxVagasComBrJobsPerQuery
	case models.PlatformLinkedIn:
		return o.cfg.MaxLinkedInJobsPerQuery
	default:
		return o.cfg.MaxJobsPerExecution
	}
}

func (o *Orchestrator) runWithRetry(ctx context.Context, s scraper.Scraper, req models.ScrapeRequest, onJob func(models.ScrapedJob) bool) error {
	var lastErr error
	for attempt := 0; attempt <= o.cfg.RetryCount; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(attempt) * time.Second
			o.logger.Info("retrying scraper", "platform", s.Platform(), "attempt", attempt, "backoff", backoff)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
		}

		if err := s.StreamJobs(ctx, req, onJob); err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	return fmt.Errorf("max retries exceeded: %w", lastErr)
}

func (o *Orchestrator) IsRunning() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.running
}

func (o *Orchestrator) LastRun() time.Time {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.lastRun
}
