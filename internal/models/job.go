package models

import (
	"time"

	"github.com/google/uuid"
)

type ScrapedJob struct {
	ID          string
	Title       string
	Company     string
	URL         string
	Location    string
	Description string
	Platform    string
}

type JobEntity struct {
	ID             uuid.UUID `db:"id"`
	PlataformJobID string    `db:"plataform_job_id"`
	Platform       string    `db:"platform"`
	Title          string    `db:"title"`
	Description    string    `db:"description"`
	URL            string    `db:"url"`
	IsApplied      bool      `db:"is_applied"`
	Status         string    `db:"status"`
	Active         bool      `db:"active"`
	CreatedBy      string    `db:"created_by"`
	LastModifiedBy string    `db:"last_modified_by"`
	CreatedAt      time.Time `db:"created_at"`
	LastModifiedAt time.Time `db:"last_modified_at"`
}

type ScrapeRequest struct {
	Query         string
	Keywords	  []string
	Location      string
	Platforms     []string
	LiAtCookie    string
	EasyApplyOnly bool
}

type ScrapeResponse struct {
	Jobs   []ScrapedJob
	Errors []string
}

type Platform string

const (
	PlatformGupy       Platform = "gupy"
	PlatformGreenhouse Platform = "greenhouse"
	PlatformVagasComBr Platform = "vagascombr"
	PlatformLinkedIn   Platform = "linkedin"
)

func AllPlatforms() []Platform {
	return []Platform{PlatformGupy, PlatformGreenhouse, PlatformVagasComBr, PlatformLinkedIn}
}
