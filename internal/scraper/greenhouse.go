package scraper

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"job-scrapper/internal/config"
	"job-scrapper/internal/models"
)

type GreenhouseScraper struct {
	cfg       *config.Config
	client    *http.Client
	logger    *slog.Logger
	companies []string
}

func NewGreenhouseScraper(cfg *config.Config, logger *slog.Logger) *GreenhouseScraper {
	return &GreenhouseScraper{
		cfg: cfg,
		client: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:       10,
				IdleConnTimeout:    30 * time.Second,
				DisableCompression: false,
			},
		},
		logger:    logger.With("scraper", "greenhouse"),
		companies: []string{
			"nubank", "ifood", "stripe", "spotify", "shopify",
			"airbnb", "uber", "notion", "figma", "vercel",
		},
	}
}

func (s *GreenhouseScraper) Platform() models.Platform {
	return models.PlatformGreenhouse
}

type greenhouseLocation struct {
	Name string `json:"name"`
}

type greenhouseJob struct {
	ID          int                `json:"id"`
	Title       string             `json:"title"`
	Location    greenhouseLocation `json:"location"`
	AbsoluteURL string             `json:"absolute_url"`
	CompanyName string             `json:"company_name"`
}

type greenhouseResponse struct {
	Jobs []greenhouseJob `json:"jobs"`
}

func (s *GreenhouseScraper) StreamJobs(ctx context.Context, req models.ScrapeRequest, onJob func(models.ScrapedJob) bool) error {
	query := req.Query
	location := req.Location
	maxJobs := s.cfg.MaxGreenhouseJobsPerQuery

	s.logger.Info("starting greenhouse scrape",
		"query", query,
		"location", location,
		"max_jobs", maxJobs,
		"companies_count", len(s.companies),
	)

	totalJobs := 0

	for _, company := range s.companies {
		select {
		case <-ctx.Done():
			s.logger.Info("greenhouse context cancelled", "company", company, "jobs_found", totalJobs)
			return ctx.Err()
		default:
		}

		s.logger.Info("fetching greenhouse company", "company", company, "query", query)

		u := fmt.Sprintf("https://boards-api.greenhouse.io/v1/boards/%s/jobs", company)

		params := url.Values{}
		if query != "" {
			params.Set("query", query)
		}
		if len(params) > 0 {
			u += "?" + params.Encode()
		}

		httpreq, err := http.NewRequestWithContext(ctx, "GET", u, nil)
		if err != nil {
			s.logger.Error("create request", "company", company, "error", err)
			continue
		}
		httpreq.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
		httpreq.Header.Set("Accept", "application/json")

		resp, err := s.client.Do(httpreq)
		if err != nil {
			s.logger.Error("fetch greenhouse", "company", company, "error", err)
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			s.logger.Warn("greenhouse non-200 response",
				"company", company,
				"status", resp.StatusCode,
				"query", query,
			)
			continue
		}

		var result greenhouseResponse
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			s.logger.Error("parse greenhouse", "company", company, "error", err)
			continue
		}

		if len(result.Jobs) == 0 {
			s.logger.Info("greenhouse company returned no jobs", "company", company, "query", query)
			s.randomDelay()
			continue
		}

		s.logger.Info("greenhouse company jobs found",
			"company", company,
			"total_available", len(result.Jobs),
			"max_for_company", maxJobs,
		)

		jobCount := 0
		for _, job := range result.Jobs {
			if jobCount >= maxJobs {
				s.logger.Info("greenhouse reached max jobs for company",
					"company", company,
					"max", maxJobs,
				)
				break
			}

			if location != "" && !ContainsIgnoreCase(job.Location.Name, location) {
				s.logger.Debug("greenhouse job filtered by location",
					"company", company,
					"job_title", job.Title,
					"job_location", job.Location.Name,
					"filter_location", location,
				)
				continue
			}

			scraped := models.ScrapedJob{
				ID:       fmt.Sprintf("gh-%s-%d", company, job.ID),
				Title:    job.Title,
				Company:  company,
				URL:      job.AbsoluteURL,
				Location: job.Location.Name,
				Platform: string(models.PlatformGreenhouse),
			}

			if !onJob(scraped) {
				s.logger.Info("greenhouse onJob returned stop", "company", company, "jobs_collected", jobCount)
				return nil
			}
			jobCount++
			totalJobs++
		}

		s.randomDelay()
	}

	s.logger.Info("greenhouse scrape finished", "total_jobs", totalJobs)
	return nil
}

func (s *GreenhouseScraper) randomDelay() {
	min := s.cfg.MinDelay()
	max := s.cfg.MaxDelay()
	if max <= min {
		time.Sleep(min)
		return
	}
	delay := min + time.Duration(time.Now().UnixNano()%int64(max-min))
	time.Sleep(delay)
}
