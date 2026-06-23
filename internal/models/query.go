package models

import (
	"sync"
)

type QueryExecutionContext struct {
	QueryID         string
	Query           string
	Location        string
	UserID          string
	Keywords        []string
	ExcludeKeywords []string
	IsActive        bool
}

type UserDailyState struct {
	UserID               string
	TodayApplicationCount int
	DailyLimit           int
}

type QueryExecutionCounters struct {
	QueryID            string
	PlatformCounters   map[string]int
	MaxPerQuery        int
	TodayQueryCount    int
	GlobalMaxPerQuery  int
	mu                 sync.Mutex
}

func NewQueryExecutionCounters(queryID string) *QueryExecutionCounters {
	return &QueryExecutionCounters{
		QueryID:          queryID,
		PlatformCounters: make(map[string]int),
	}
}

func (c *QueryExecutionCounters) CanScrapePlatform(platform string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.PlatformCounters[platform] < c.GlobalMaxPerQuery
}

func (c *QueryExecutionCounters) IncrementPlatform(platform string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.PlatformCounters[platform]++
}

func (c *QueryExecutionCounters) GetPlatformCounters() map[string]int {
	c.mu.Lock()
	defer c.mu.Unlock()
	result := make(map[string]int, len(c.PlatformCounters))
	for k, v := range c.PlatformCounters {
		result[k] = v
	}
	return result
}
