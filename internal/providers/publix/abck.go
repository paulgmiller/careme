package publix

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"

	"careme/internal/brightdata"
	"careme/internal/cache"
)

const (
	abckLatestCacheKey      = "publix/abck/latest.json"
	abckHistoryPrefix       = "publix/abck/history/"
	brightDataBrowserSource = "brightdata-browser-api"
)

type abckRecord struct {
	Cookie    string     `json:"cookie"`
	FetchedAt time.Time  `json:"fetched_at"`
	SourceURL string     `json:"source_url"`
	Provider  string     `json:"provider"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

type abckBrowser interface {
	Cookies(ctx context.Context, targetURL string, opts brightdata.BrowserOptions) ([]brightdata.BrowserCookie, error)
}

type AbckParams struct {
	TargetURL           string
	WaitAfterNavigation time.Duration
}

func FetchAbck(ctx context.Context, browser abckBrowser, params AbckParams) (abckRecord, error) {
	if browser == nil {
		return abckRecord{}, errors.New("browser is required")
	}

	params.TargetURL = strings.TrimSpace(params.TargetURL)
	if params.TargetURL == "" {
		return abckRecord{}, errors.New("target URL is required")
	}

	cookies, err := browser.Cookies(ctx, params.TargetURL, brightdata.BrowserOptions{
		WaitAfterNavigation: params.WaitAfterNavigation,
	})
	if err != nil {
		return abckRecord{}, fmt.Errorf("browser cookie fetch: %w", err)
	}

	cookie, ok := brightdata.CookieNamed(cookies, "_abck")
	if !ok {
		return abckRecord{}, fmt.Errorf("cookie %q not found in browser session", "_abck")
	}

	record := abckRecord{
		Cookie:    cookie.Value,
		FetchedAt: time.Now().UTC(),
		SourceURL: params.TargetURL,
		Provider:  brightDataBrowserSource,
	}
	if cookie.Expires != nil {
		expiresAt := *cookie.Expires
		record.ExpiresAt = &expiresAt
	}
	return record, nil
}

func SaveAbckRecord(ctx context.Context, c cache.Cache, record abckRecord) error {
	if c == nil {
		return errors.New("cache is required")
	}

	record.Cookie = strings.TrimSpace(record.Cookie)
	if record.Cookie == "" {
		return errors.New("cookie is required")
	}

	body, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("marshal abck record: %w", err)
	}

	historyKey := path.Join(abckHistoryPrefix, record.FetchedAt.Format(time.RFC3339Nano)+".json")
	if err := c.Put(ctx, historyKey, string(body), cache.Unconditional()); err != nil {
		return fmt.Errorf("write abck history: %w", err)
	}
	if err := c.Put(ctx, abckLatestCacheKey, string(body), cache.Unconditional()); err != nil {
		return fmt.Errorf("write abck latest: %w", err)
	}
	return nil
}

func loadLatestAbck(ctx context.Context, c cache.Cache) (*abckRecord, error) {
	if c == nil {
		return nil, errors.New("cache is required")
	}

	reader, err := c.Get(ctx, abckLatestCacheKey)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = reader.Close()
	}()

	var record abckRecord
	if err := json.NewDecoder(reader).Decode(&record); err != nil {
		return nil, fmt.Errorf("decode abck record: %w", err)
	}
	record.Cookie = strings.TrimSpace(record.Cookie)
	if record.Cookie == "" {
		return nil, fmt.Errorf("decode abck record: cookie is empty")
	}
	return &record, nil
}
