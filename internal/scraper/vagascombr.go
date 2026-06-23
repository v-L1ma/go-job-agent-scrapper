package scraper

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	"job-scrapper/internal/browser"
	"job-scrapper/internal/config"
	"job-scrapper/internal/models"

	"github.com/playwright-community/playwright-go"
	"golang.org/x/sync/errgroup"
)

type VagasComBrScraper struct {
	BaseScraper
	logger  *slog.Logger
}

func NewVagasComBrScraper(cfg *config.Config, bm *browser.Manager, logger *slog.Logger) *VagasComBrScraper {
	return &VagasComBrScraper{
		BaseScraper: NewBaseScraper(cfg, bm),
		logger:      logger.With("scraper", "vagascombr"),
	}
}

func (s *VagasComBrScraper) Platform() models.Platform {
	return models.PlatformVagasComBr
}

func (s *VagasComBrScraper) StreamJobs(ctx context.Context, req models.ScrapeRequest, onJob func(models.ScrapedJob) bool) error {
	keywords := req.Keywords
	if len(req.Keywords) == 0 {
		keywords = []string{req.Query}
	}

	s.logger.Info("starting vagascombr scrape",
		"query", req.Query,
		"keywords", keywords,
		"location", req.Location,
		"max_jobs", s.cfg.MaxVagasComBrJobsPerQuery,
	)

	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(2)

	for _, kw := range keywords {
		kw := kw
		g.Go(func() error {
			s.logger.Info("vagasComBr processing keyword", "keyword", kw)
			return s.scrapeKeyword(ctx, kw, req.Location, onJob)
		})
	}

	return g.Wait()
}

func (s *VagasComBrScraper) scrapeKeyword(ctx context.Context, keyword, location string, onJob func(models.ScrapedJob) bool) error {
	browserCtx, err := s.browser.AcquireContext()
	if err != nil {
		return fmt.Errorf("acquire context: %w", err)
	}
	defer s.browser.ReleaseContext(browserCtx)

	page, err := s.browser.NewPage(browserCtx)
	if err != nil {
		return fmt.Errorf("new page: %w", err)
	}
	defer page.Close()

	if err := page.Route("**/*", func(route playwright.Route) {
		resourceType := route.Request().ResourceType()
		if resourceType == "image" || resourceType == "media" || resourceType == "font" {
			route.Abort()
		} else {
			route.Continue()
		}
	}); err != nil {
		return fmt.Errorf("set route: %w", err)
	}

	searchURL := fmt.Sprintf("https://www.vagas.com.br/vagas-de-%s", url.PathEscape(keyword))
	if location != "" {
		searchURL += fmt.Sprintf("?onde=%s", url.QueryEscape(location))
	}

	s.logger.Info("vagascombr navigating", "keyword", keyword, "url", searchURL)

	if _, err := page.Goto(searchURL, playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateDomcontentloaded,
	}); err != nil {
		return fmt.Errorf("navigate to %s: %w", searchURL, err)
	}

	s.logger.Info("vagascombr page loaded", "keyword", keyword, "url", searchURL)

	jobCount := 0
	maxJobs := s.cfg.MaxVagasComBrJobsPerQuery

	s.logger.Info("vagascombr keyword scrape starting",
		"keyword", keyword,
		"max_jobs", maxJobs,
	)

	select {
	case <-ctx.Done():
		s.logger.Info("vagascombr context cancelled before processing", "keyword", keyword)
		return ctx.Err()
	default:
	}

	cards, err := page.QuerySelectorAll("li.vaga")
	if err != nil {
		return fmt.Errorf("query job cards: %w", err)
	}

	s.logger.Info("vagascombr cards found",
		"keyword", keyword,
		"cards_found", len(cards),
	)

	if len(cards) == 0 {
		s.logger.Warn("vagascombr found zero job cards - possible selector mismatch or site change",
			"keyword", keyword,
		)
		return nil
	}

	for _, card := range cards {
		select {
		case <-ctx.Done():
			s.logger.Info("vagascombr context cancelled", "keyword", keyword, "jobs_found", jobCount)
			return ctx.Err()
		default:
		}

		if jobCount >= maxJobs {
			s.logger.Info("vagascombr reached max jobs", "keyword", keyword, "max", maxJobs)
			break
		}

		titleEl, _ := card.QuerySelector("h2.cargo a.link-detalhes-vaga")
		companyEl, _ := card.QuerySelector("span.emprVaga")
		locEl, _ := card.QuerySelector("footer div.vaga-local")
		descEl, _ := card.QuerySelector("div.detalhes p")

		var title, company, jobURL, jobLocation, jobDescription, jobID string

		if titleEl != nil {
			title, _ = titleEl.TextContent()
			href, _ := titleEl.GetAttribute("href")
			dataID, _ := titleEl.GetAttribute("data-id-vaga")
			if href != "" {
				if strings.HasPrefix(href, "http") {
					jobURL = href
				} else {
					jobURL = "https://www.vagas.com.br" + href
				}
			}
			if dataID != "" {
				jobID = fmt.Sprintf("vcbr-%s", dataID)
			}
		}

		if companyEl != nil {
			company, _ = companyEl.TextContent()
		}

		if locEl != nil {
			jobLocation, _ = locEl.TextContent()
		}

		if descEl != nil {
			jobDescription, _ = descEl.TextContent()
		}

		if title == "" || jobID == "" {
			s.logger.Debug("vagascombr skipping card with empty title or id", "keyword", keyword)
			continue
		}

		scraped := models.ScrapedJob{
			ID:          jobID,
			Title:       strings.TrimSpace(title),
			Company:     strings.TrimSpace(company),
			Location:    strings.TrimSpace(jobLocation),
			Description: strings.TrimSpace(jobDescription),
			URL:         jobURL,
			Platform:    string(models.PlatformVagasComBr),
		}

		s.logger.Info("vagascombr job found",
			"keyword", keyword,
			"job_id", scraped.ID,
			"title", scraped.Title,
			"company", scraped.Company,
			"location", scraped.Location,
		)

		if !onJob(scraped) {
			s.logger.Info("vagascombr onJob returned stop", "keyword", keyword, "jobs_collected", jobCount)
			return nil
		}
		jobCount++
	}

	s.logger.Info("vagascombr keyword scrape finished",
		"keyword", keyword,
		"jobs_found", jobCount,
	)
	return nil
}
