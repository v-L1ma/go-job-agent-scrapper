package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	PostgresDSN               string
	Headless                  bool
	IntervalMinutes           int
	MaxJobsPerExecution       int
	MaxApplicationsPerDay     int
	MaxLinkedInJobsPerQuery   int
	MaxGupyJobsPerQuery       int
	MaxVagasComBrJobsPerQuery int
	MaxGreenhouseJobsPerQuery int
	LiAtCookie                string
	EasyApplyOnly             bool
	MinDelayMs                int
	MaxDelayMs                int
	RetryCount                int
	ScreenshotsPath           string
	LogLevel                  string
	BrowserBinPath            string
	DisableBrowserScrapers    bool
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return fallback
}

func Load() *Config {
	return &Config{
		PostgresDSN:               getEnv("DATABASE_URL", ""),
		Headless:                  getEnvBool("HEADLESS", true),
		IntervalMinutes:           getEnvInt("SCRAPER_INTERVAL", 30),
		MaxJobsPerExecution:       getEnvInt("MAX_JOBS_PER_EXECUTION", 100),
		MaxApplicationsPerDay:     getEnvInt("MAX_APPS_PER_DAY", 100),
		MaxLinkedInJobsPerQuery:   getEnvInt("MAX_LINKEDIN_JOBS", 15),
		MaxGupyJobsPerQuery:       getEnvInt("MAX_GUPY_JOBS", 10),
		MaxVagasComBrJobsPerQuery: getEnvInt("MAX_VAGAS_JOBS", 10),
		MaxGreenhouseJobsPerQuery: getEnvInt("MAX_GREENHOUSE_JOBS", 10),
		LiAtCookie:                getEnv("LI_AT_COOKIE", ""),
		EasyApplyOnly:             getEnvBool("EASY_APPLY_ONLY", false),
		MinDelayMs:                getEnvInt("MIN_DELAY_MS", 0),
		MaxDelayMs:                getEnvInt("MAX_DELAY_MS", 0),
		RetryCount:                getEnvInt("RETRY_COUNT", 2),
		ScreenshotsPath:           getEnv("SCREENSHOTS_PATH", "./screenshots"),
		LogLevel:                  getEnv("LOG_LEVEL", "info"),
		BrowserBinPath:            getEnv("PLAYWRIGHT_BROWSERS_PATH", ""),
		DisableBrowserScrapers:    getEnvBool("DISABLE_BROWSER_SCRAPERS", false),
	}
}

func (c *Config) MinDelay() time.Duration {
	return time.Duration(c.MinDelayMs) * time.Millisecond
}

func (c *Config) MaxDelay() time.Duration {
	return time.Duration(c.MaxDelayMs) * time.Millisecond
}
