package scraper

import (
	"context"
	"math/rand"
	"time"

	"job-scrapper/internal/browser"
	"job-scrapper/internal/config"
	"job-scrapper/internal/models"
)

type Scraper interface {
	StreamJobs(ctx context.Context, req models.ScrapeRequest, onJob func(models.ScrapedJob) bool) error
	Platform() models.Platform
}

type BaseScraper struct {
	cfg     *config.Config
	browser *browser.Manager
}

func NewBaseScraper(cfg *config.Config, bm *browser.Manager) BaseScraper {
	return BaseScraper{cfg: cfg, browser: bm}
}

func (s *BaseScraper) RandomDelay() {
	min := s.cfg.MinDelay()
	max := s.cfg.MaxDelay()
	delay := min + time.Duration(rand.Int63n(int64(max-min+1)))
	time.Sleep(delay)
}

func NormalizeText(s string) string {
	if len(s) == 0 {
		return ""
	}
	b := make([]byte, 0, len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
			b = append(b, byte(r))
		case r >= 'A' && r <= 'Z':
			b = append(b, byte(r+'a'-'A'))
		case r == ' ' || r == '-':
			b = append(b, ' ')
		}
	}
	return string(b)
}

func ContainsIgnoreCase(s, substr string) bool {
	if len(substr) == 0 {
		return true
	}
	if len(s) < len(substr) {
		return false
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		match := true
		for j := 0; j < len(substr); j++ {
			sc := s[i+j]
			tc := substr[j]
			if sc >= 'A' && sc <= 'Z' {
				sc += 32
			}
			if tc >= 'A' && tc <= 'Z' {
				tc += 32
			}
			if sc != tc {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func ParseKeywords(keywords string) []string {
	var result []string
	current := make([]byte, 0)
	for i := 0; i < len(keywords); i++ {
		if keywords[i] == ',' {
			if len(current) > 0 {
				result = append(result, string(current))
				current = current[:0]
			}
		} else if keywords[i] != ' ' {
			current = append(current, keywords[i])
		}
	}
	if len(current) > 0 {
		result = append(result, string(current))
	}
	return result
}
