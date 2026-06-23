package browser

import (
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/playwright-community/playwright-go"
)

type Manager struct {
	mu          sync.Mutex
	pw          *playwright.Playwright
	browser     playwright.Browser
	contextPool []playwright.BrowserContext
	poolSize    int
	idleTimeout time.Duration
	headless    bool
	browserPath string
	closed      bool
}

func NewManager(headless bool, browserPath string) *Manager {
	return &Manager{
		poolSize:    3,
		idleTimeout: 20 * time.Minute,
		headless:    headless,
		browserPath: browserPath,
	}
}

func (m *Manager) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.pw != nil {
		return nil
	}

	if m.browserPath != "" {
		os.Setenv("PLAYWRIGHT_BROWSERS_PATH", m.browserPath)
		log.Printf("using PLAYWRIGHT_BROWSERS_PATH: %s", m.browserPath)
	}

	if err := playwright.Install(); err != nil {
		return fmt.Errorf("install playwright: %w", err)
	}

	pw, err := playwright.Run()
	if err != nil {
		return fmt.Errorf("start playwright: %w", err)
	}
	m.pw = pw

	browser, err := pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		Headless: playwright.Bool(m.headless),
	})
	if err != nil {
		pw.Stop()
		return fmt.Errorf("launch chromium: %w", err)
	}
	m.browser = browser

	execPath := pw.Chromium.ExecutablePath()
	log.Printf("browser manager started (path: %s, headless: %v)", execPath, m.headless)
	return nil
}

func (m *Manager) AcquireContext() (playwright.BrowserContext, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return nil, fmt.Errorf("browser manager is closed")
	}

	if len(m.contextPool) > 0 {
		ctx := m.contextPool[len(m.contextPool)-1]
		m.contextPool = m.contextPool[:len(m.contextPool)-1]
		return ctx, nil
	}

	ctx, err := m.browser.NewContext()
	if err != nil {
		return nil, fmt.Errorf("create browser context: %w", err)
	}

	return ctx, nil
}

func (m *Manager) ReleaseContext(ctx playwright.BrowserContext) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		ctx.Close()
		return
	}

	if len(m.contextPool) >= m.poolSize {
		ctx.Close()
		return
	}

	m.contextPool = append(m.contextPool, ctx)
}

func (m *Manager) NewPage(ctx playwright.BrowserContext) (playwright.Page, error) {
	page, err := ctx.NewPage()
	if err != nil {
		return nil, fmt.Errorf("create page: %w", err)
	}

	return page, nil
}

func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return
	}
	m.closed = true

	for _, ctx := range m.contextPool {
		ctx.Close()
	}
	m.contextPool = nil

	if m.browser != nil {
		m.browser.Close()
	}
	if m.pw != nil {
		m.pw.Stop()
	}

	log.Println("browser manager closed")
}
