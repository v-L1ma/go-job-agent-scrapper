package scraper

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"job-scrapper/internal/config"
	"job-scrapper/internal/models"

	"golang.org/x/sync/errgroup"
)

type GupyScraper struct {
	cfg     *config.Config
	client  *http.Client
	logger  *slog.Logger
}

func NewGupyScraper(cfg *config.Config, logger *slog.Logger) *GupyScraper {
	return &GupyScraper{
		cfg: cfg,
		client: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        10,
				IdleConnTimeout:     30 * time.Second,
				DisableCompression:  false,
			},
		},
		logger: logger.With("scraper", "gupy"),
	}
}

func (s *GupyScraper) Platform() models.Platform {
	return models.PlatformGupy
}

type gupyJob struct {
	ID             int      `json:"id"`
	CompanyId      int  `json:"companyId"`
	Name           string   `json:"name"`
	Description	   string   `json:"description"`
	CareerPageId  int   `json:"careerPageId"`
	CareerPageName string   `json:"careerPageName"`
	City           string   `json:"city"`
	State          string   `json:"state"`
	Country          string   `json:"country"`
	CareerPageLogo  string   `json:"careerPageLogo"`
	CareerPageUrl  string   `json:"careerPageUrl"`
	Type  string   `json:"type"`
	IsRemoteWork  bool   `json:"isRemoteWork"`
	JobUrl  string   `json:"jobUrl"`
	PublishedDate  string   `json:"publishedDate"`
}

type gupyPagination struct {
	Total int `json:"total"`
	Limit int `json:"limit"`
	Offset int `json:"offset"`
}

type gupyResponse struct {
	Data          []gupyJob `json:"data"`
	Pagination    int       `json:"totalCount"`
	NextPageToken string    `json:"nextPageToken"`
}

func (s *GupyScraper) StreamJobs(ctx context.Context, req models.ScrapeRequest, onJob func(models.ScrapedJob) bool) error {
	keywords := req.Keywords
	if len(req.Keywords) == 0 {
		keywords = []string{req.Query}
	}

	s.logger.Info("starting gupy scrape",
		"query", req.Query,
		"keywords", keywords,
		"location", req.Location,
		"max_jobs", s.cfg.MaxGupyJobsPerQuery,
	)

	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(3)

	for _, kw := range keywords {
		kw := kw
		g.Go(func() error {
			s.logger.Info("gupy processing keyword", "keyword", kw)
			return s.fetchKeyword(ctx, kw, req.Location, onJob)
		})
	}

	return g.Wait()
}

func (s *GupyScraper) fetchKeyword(ctx context.Context, keyword, location string, onJob func(models.ScrapedJob) bool) error {
	offset := 0
	limit := 50
	maxJobs := s.cfg.MaxGupyJobsPerQuery
	jobCount := 0
	totalAvailable := 0

	s.logger.Info("gupy fetch keyword starting",
		"keyword", keyword,
		"max_jobs", maxJobs,
		"batch_limit", limit,
	)

	for {
		select {
		case <-ctx.Done():
			s.logger.Info("gupy context cancelled", "keyword", keyword, "jobs_found", jobCount)
			return ctx.Err()
		default:
		}

		if jobCount >= maxJobs {
			break
		}

		params := url.Values{}
		params.Set("jobName", keyword)
		params.Set("limit", fmt.Sprintf("%d", limit))
		params.Set("offset", fmt.Sprintf("%d", offset))

		apiURL := fmt.Sprintf("https://employability-portal.gupy.io/api/v1/jobs?%s", params.Encode())

		s.logger.Info("gupy fetching page",
			"keyword", keyword,
			"offset", offset,
			"limit", limit,
			"url", apiURL,
		)

		req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
		if err != nil {
			return fmt.Errorf("create request: %w", err)
		}
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
		req.Header.Set("Accept", "application/json")

		resp, err := s.client.Do(req)
		if err != nil {
			return fmt.Errorf("fetch gupy: %w", err)
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return fmt.Errorf("read body: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("gupy API returned status %d for keyword %s", resp.StatusCode, keyword)
		}

		var result gupyResponse
		if err := json.Unmarshal(body, &result); err != nil {
			return fmt.Errorf("parse gupy response: %w", err)
		}

		totalAvailable = result.Pagination
		s.logger.Info("gupy API response",
			"keyword", keyword,
			"total_available", result.Pagination,
			"returned_count", len(result.Data),
			"offset", offset,
		)

		if len(result.Data) == 0 {
			s.logger.Info("gupy no more data", "keyword", keyword, "offset", offset)
			break
		}

		for _, job := range result.Data {
			if jobCount >= maxJobs {
				break
			}

			if location != "" && !s.matchLocation(job, location) {
				s.logger.Debug("gupy job filtered by location",
					"keyword", keyword,
					"job_title", job.Name,
					"job_city", job.City,
					"job_state", job.State,
					"filter_location", location,
				)
				continue
			}

			scraped := models.ScrapedJob{
				ID:       strconv.Itoa(job.ID),
				Title:    job.Name,
				Company:  job.CareerPageName,
				URL:      job.JobUrl,
				Location: fmt.Sprintf("%s, %s", job.City, job.State),
				Platform: string(models.PlatformGupy),
			}

			s.logger.Info("gupy job found",
				"keyword", keyword,
				"job_id", scraped.ID,
				"title", scraped.Title,
				"company", scraped.Company,
				"location", scraped.Location,
			)

			if !onJob(scraped) {
				s.logger.Info("gupy onJob returned stop", "keyword", keyword, "jobs_collected", jobCount)
				return nil
			}
			jobCount++
		}

		offset += limit

		if result.NextPageToken == "" && len(result.Data) < limit {
			s.logger.Info("gupy no more pages", "keyword", keyword, "total_collected", jobCount)
			break
		}
	}

	s.logger.Info("gupy keyword scrape finished",
		"keyword", keyword,
		"jobs_found", jobCount,
		"total_available", totalAvailable,
	)
	return nil
}

func (s *GupyScraper) matchLocation(job gupyJob, location string) bool {
	normalized := NormalizeText(location)
	city := NormalizeText(job.City)
	state := NormalizeText(job.State)

	return city == normalized || state == normalized
}
