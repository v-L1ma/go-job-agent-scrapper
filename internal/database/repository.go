package database

import (
	"context"
	"fmt"
	"time"

	"job-scrapper/internal/models"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) LoadQueryContexts(ctx context.Context) ([]models.QueryExecutionContext, error) {
	query := `
		SELECT
			usq."SearchQueryId",
			sq."Query",
			COALESCE(p."Location", '') as "Location",
			usq."UserId",
			sq."Keywords",
			sq."Active"
		FROM "UserSearchQueries" usq
		JOIN "SearchQueries" sq ON sq."Id" = usq."SearchQueryId"
		LEFT JOIN "Preferences" p ON p."UserId" = usq."UserId" AND p."Active" = true
		WHERE sq."Active" = true
	`

	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("load query contexts: %w", err)
	}
	defer rows.Close()

	var contexts []models.QueryExecutionContext
	for rows.Next() {
		var c models.QueryExecutionContext
		if err := rows.Scan(
			&c.QueryID,
			&c.Query,
			&c.Location,
			&c.UserID,
			&c.Keywords,
			&c.IsActive,
		); err != nil {
			return nil, fmt.Errorf("scan query context: %w", err)
		}
		contexts = append(contexts, c)
	}

	return contexts, nil
}

func (r *Repository) LoadUserDailyStates(ctx context.Context, userIDs []string) (map[string]*models.UserDailyState, error) {
	if len(userIDs) == 0 {
		return make(map[string]*models.UserDailyState), nil
	}

	query := `
		SELECT
			u."Id",
			COALESCE(COUNT(j."Id") FILTER (WHERE j."CreatedAt"::date = CURRENT_DATE AND j."IsApplied" = true), 0)
		FROM "AspNetUsers" u
		LEFT JOIN "Jobs" j ON j."CreatedBy" = u."Id"::text
		WHERE u."Id" = ANY($1)
		GROUP BY u."Id"
	`

	userUUIDs := make([]uuid.UUID, len(userIDs))
	for i, id := range userIDs {
		uid, err := uuid.Parse(id)
		if err != nil {
			return nil, fmt.Errorf("parse user UUID %s: %w", id, err)
		}
		userUUIDs[i] = uid
	}

	rows, err := r.pool.Query(ctx, query, userUUIDs)
	if err != nil {
		return nil, fmt.Errorf("load user daily states: %w", err)
	}
	defer rows.Close()

	states := make(map[string]*models.UserDailyState)
	for rows.Next() {
		var uid uuid.UUID
		var s models.UserDailyState
		if err := rows.Scan(&uid, &s.TodayApplicationCount); err != nil {
			return nil, fmt.Errorf("scan user daily state: %w", err)
		}
		s.UserID = uid.String()
		s.DailyLimit = 100
		states[s.UserID] = &s
	}

	return states, nil
}

func (r *Repository) JobExistsByPlatformID(ctx context.Context, platformJobID string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM "Jobs" WHERE "PlataformJobId" = $1)`,
		platformJobID,
	).Scan(&exists)
	return exists, err
}

func (r *Repository) InsertJob(ctx context.Context, job *models.ScrapedJob, userID string) error {
	id := uuid.New()
	now := time.Now().UTC()

	_, err := r.pool.Exec(ctx, `
		INSERT INTO "Jobs" (
			"Id", "PlataformJobId", "Platform", "Title", "Company", "Description", "Url",
			"IsApplied", "Status", "Active", "CreatedBy", "LastModifiedBy",
			"CreatedAt", "LastModifiedAt"
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7,
			false, 'pending', true, $8, $8,
			$9, $9
		)
	`, id, job.ID, job.Platform, job.Title, job.Company, job.Description, job.URL, userID, now)
	if err != nil {
		return fmt.Errorf("insert job: %w", err)
	}

	return nil
}

func (r *Repository) SaveJob(ctx context.Context, job *models.ScrapedJob, queryID string, userDailyState *models.UserDailyState, counters *models.QueryExecutionCounters, excludeKeywords []string) (bool, error) {
	if job.ID == "" || job.URL == "" {
		return false, nil
	}

	if userDailyState != nil && userDailyState.TodayApplicationCount >= userDailyState.DailyLimit {
		return false, nil
	}

	for _, kw := range excludeKeywords {
		if containsKeyword(job.Title, kw) || containsKeyword(job.Description, kw) {
			return false, nil
		}
	}

	if !counters.CanScrapePlatform(job.Platform) {
		return false, nil
	}

	exists, err := r.JobExistsByPlatformID(ctx, job.ID)
	if err != nil {
		return false, err
	}
	if exists {
		return false, nil
	}

	if err := r.InsertJob(ctx, job, userDailyState.UserID); err != nil {
		return false, err
	}

	counters.IncrementPlatform(job.Platform)
	return true, nil
}

func (r *Repository) UpdateUserSearchQueryLimits(ctx context.Context, queryID string, platformCounts map[string]int) error {
	total := 0
	for _, count := range platformCounts {
		total += count
	}
	if total == 0 {
		return nil
	}

	queryIDUUID, err := uuid.Parse(queryID)
	if err != nil {
		return fmt.Errorf("parse query UUID %s: %w", queryID, err)
	}

	_, err = r.pool.Exec(ctx, `
		UPDATE "UserSearchQueries"
		SET "SavedJobsCount" = COALESCE("SavedJobsCount", 0) + $1
		WHERE "SearchQueryId" = $2
	`, total, queryIDUUID)
	if err != nil {
		return fmt.Errorf("update query limits: %w", err)
	}

	return nil
}

func (r *Repository) Close() {
	r.pool.Close()
}

func containsKeyword(text, keyword string) bool {
	return len(text) >= len(keyword) && containsIgnoreCase(text, keyword)
}

func containsIgnoreCase(s, substr string) bool {
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
