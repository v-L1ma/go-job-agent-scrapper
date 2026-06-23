package scraper

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"job-scrapper/internal/browser"
	"job-scrapper/internal/config"
	"job-scrapper/internal/models"

	"github.com/playwright-community/playwright-go"
)

type LinkedInScraper struct {
	BaseScraper
	logger  *slog.Logger
}

func NewLinkedInScraper(cfg *config.Config, bm *browser.Manager, logger *slog.Logger) *LinkedInScraper {
	return &LinkedInScraper{
		BaseScraper: NewBaseScraper(cfg, bm),
		logger:      logger.With("scraper", "linkedin"),
	}
}

func (s *LinkedInScraper) Platform() models.Platform {
	return models.PlatformLinkedIn
}

func (s *LinkedInScraper) StreamJobs(ctx context.Context, req models.ScrapeRequest, onJob func(models.ScrapedJob) bool) error {
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

	searchQuery := url.QueryEscape(req.Query)
	searchURL := fmt.Sprintf("https://www.linkedin.com/jobs/search/?keywords=%s&location=Brasil&geoId=106057199&f_AL=true", searchQuery)

	s.logger.Info("navigating to LinkedIn", "url", searchURL, "query", req.Query)

	if _, err := page.Goto(searchURL, playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateNetworkidle,
	}); err != nil {
		return fmt.Errorf("navigate to linkedin: %w", err)
	}

	s.logger.Info("linkedin page loaded", "query", req.Query, "url", searchURL)

	time.Sleep(3 * time.Second)

	if err := s.dismissLoginModal(page); err != nil {
		s.logger.Warn("failed to check/dismiss login modal", "error", err)
	}

	time.Sleep(1 * time.Second)

	jobCount := 0
	maxJobs := s.cfg.MaxLinkedInJobsPerQuery
	pageCount := 0

	s.logger.Info("linkedin scrape starting", "max_jobs", maxJobs, "query", req.Query)

	for {
		pageCount++
		select {
		case <-ctx.Done():
			s.logger.Info("linkedin context cancelled", "jobs_found", jobCount, "pages_seen", pageCount)
			return ctx.Err()
		default:
		}

		if _, err := page.Evaluate("window.scrollTo(0, document.body.scrollHeight)"); err != nil {
			return fmt.Errorf("scroll: %w", err)
		}

		time.Sleep(2 * time.Second)

		cards, err := page.QuerySelectorAll(".job-search-card")
		if err != nil {
			return fmt.Errorf("query cards: %w", err)
		}

		s.logger.Info("linkedin page cards found",
			"page", pageCount,
			"cards_found", len(cards),
			"jobs_collected_so_far", jobCount,
			"max_jobs", maxJobs,
			"query", req.Query,
		)

		if len(cards) == 0 {
			s.logger.Warn("linkedin found zero job cards - possible login wall or selector mismatch",
				"page", pageCount,
				"query", req.Query,
			)
			break
		}

		for _, card := range cards {
			if jobCount >= maxJobs {
				s.logger.Info("linkedin reached max jobs", "max", maxJobs, "query", req.Query)
				break
			}

			if err := card.Click(); err != nil {
				s.logger.Debug("linkedin card click failed", "error", err)
				continue
			}

			time.Sleep(1 * time.Second)

			titleEl, _ := card.QuerySelector("h3.base-search-card__title")
			companyEl, _ := card.QuerySelector("h4.base-search-card__subtitle")
			locationEl, _ := card.QuerySelector("span.job-search-card__location")
			linkEl, _ := card.QuerySelector("a.base-card__full-link")
			entityUrn, _ := card.GetAttribute("data-entity-urn")

			descEl, _ := page.QuerySelector(".job-details-jobs-unified-top-card__description, .jobs-description__content, .show-more-less-html__markup, .jobs-description")

			title, _ := titleEl.TextContent()
			company, _ := companyEl.TextContent()
			location, _ := locationEl.TextContent()
			var description string
			if descEl != nil {
				description, _ = descEl.TextContent()
			}

			jobID := fmt.Sprintf("li-%s-%d", req.Query, jobCount)
			jobURL := ""
			if entityUrn != "" {
				parts := strings.Split(entityUrn, ":")
				id := parts[len(parts)-1]
				jobID = fmt.Sprintf("li-%s", id)
				jobURL = fmt.Sprintf("https://www.linkedin.com/jobs/view/%s/", id)
			}
			if jobURL == "" && linkEl != nil {
				href, _ := linkEl.GetAttribute("href")
				if href != "" {
					jobURL = href
				}
			}

			scraped := models.ScrapedJob{
				ID:          jobID,
				Title:       strings.TrimSpace(title),
				Company:     strings.TrimSpace(company),
				URL:         jobURL,
				Location:    strings.TrimSpace(location),
				Description: strings.TrimSpace(description),
				Platform:    string(models.PlatformLinkedIn),
			}

			if !onJob(scraped) {
				s.logger.Info("linkedin onJob returned stop", "jobs_collected", jobCount, "query", req.Query)
				return nil
			}
			jobCount++

			s.RandomDelay()
		}

		if jobCount >= maxJobs {
			s.logger.Info("linkedin reached max jobs, stopping pagination", "max", maxJobs, "query", req.Query)
			break
		}

		showMoreBtn, err := page.QuerySelector("button.infinite-scroller__show-more-button")
		if err != nil || showMoreBtn == nil {
			s.logger.Info("linkedin no show-more button found", "page", pageCount, "query", req.Query)
			break
		}

		visible, _ := showMoreBtn.IsVisible()
		if !visible {
			s.logger.Info("linkedin show-more button is hidden (all jobs loaded)", "page", pageCount, "query", req.Query)
			break
		}

		s.logger.Info("linkedin clicking show-more to load additional jobs", "page", pageCount, "query", req.Query)
		if err := showMoreBtn.Click(); err != nil {
			s.logger.Warn("linkedin show-more click failed", "page", pageCount, "error", err, "query", req.Query)
			break
		}

		time.Sleep(3 * time.Second)
	}

	s.logger.Info("linkedin scrape finished",
		"query", req.Query,
		"jobs_found", jobCount,
		"pages_seen", pageCount,
		"max_jobs", maxJobs,
	)
	return nil
}

func (s *LinkedInScraper) dismissLoginModal(page playwright.Page) error {
	modalSelectors := []string{
		".contextual-sign-in-modal",
		".modal__main",
		".artdeco-modal",
		"div[role='dialog']",
		"div[data-test-modal-id='base-contextual-sign-in-modal']",
		"#base-contextual-sign-in-modal",
	}

	modalPresent := false
	var modalEl playwright.ElementHandle
	for _, sel := range modalSelectors {
		el, err := page.QuerySelector(sel)
		if err == nil && el != nil {
			modalPresent = true
			modalEl = el
			break
		}
	}

	if !modalPresent {
		return nil
	}

	s.logger.Info("login modal detected, attempting to dismiss")

	if modalEl != nil {
		closeBtn, _ := modalEl.QuerySelector(".artdeco-modal__dismiss, button[aria-label='Fechar'], button[aria-label='Close'], button[aria-label='Dismiss'], button[class*='dismiss'], button[class*='close']")
		if closeBtn != nil {
			s.logger.Info("found close button on modal, clicking")
			if err := closeBtn.Click(); err == nil {
				time.Sleep(1 * time.Second)
				if el, _ := page.QuerySelector(".contextual-sign-in-modal, .artdeco-modal"); el == nil {
					s.logger.Info("login modal dismissed via close button")
					return nil
				}
			}
		}
	}

	for i := 0; i < 3; i++ {
		if err := page.Keyboard().Press("Escape"); err == nil {
			time.Sleep(500 * time.Millisecond)
			if el, _ := page.QuerySelector(".contextual-sign-in-modal, .artdeco-modal, .modal__main"); el == nil {
				s.logger.Info("login modal dismissed via Escape key")
				return nil
			}
		}
	}

	s.logger.Info("gentle dismiss failed, force-removing modal via JavaScript")
	_, err := page.Evaluate(`
		(function() {
			var overlays = document.querySelectorAll('.artdeco-modal-overlay, [data-test-modal-overlay], .artdeco-modal-overlay--background');
			overlays.forEach(function(el) { el.remove(); });

			var modals = document.querySelectorAll('.artdeco-modal, [role="dialog"], .contextual-sign-in-modal');
			modals.forEach(function(el) {
				var parent = el.closest('.artdeco-modal, [role="dialog"]') || el;
				parent.remove();
			});

			var modalContainers = document.querySelectorAll('.modal__main');
			modalContainers.forEach(function(el) {
				var parent = el.closest('.artdeco-modal, [role="dialog"]');
				if (parent) { parent.remove(); }
				else { el.remove(); }
			});

			document.body.style.overflow = '';
			document.body.style.position = '';
			document.documentElement.style.overflow = '';
			document.querySelectorAll('[aria-hidden="true"]').forEach(function(el) {
				if (el !== document.body && el !== document.documentElement) {
					el.removeAttribute('aria-hidden');
				}
			});
		})();
	`)
	if err != nil {
		s.logger.Warn("force-remove modal JS failed", "error", err)
	}

	time.Sleep(1 * time.Second)

	if el, _ := page.QuerySelector(".contextual-sign-in-modal, .artdeco-modal, .modal__main"); el != nil {
		s.logger.Warn("login modal still present after force removal — consider setting LI_AT_COOKIE for authenticated access")
	} else {
		s.logger.Info("login modal force-removed successfully")
	}

	return nil
}
