package albertsons

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path"
	"strings"
	"time"

	"careme/internal/cache"
)

const (
	Reese84LatestCacheKey   = "albertsons/reese84/latest.json"
	Reese84HistoryPrefix    = "albertsons/reese84/history/"
	brightDataBrowserSource = "brightdata-browser-api"
)

type CookieRecord struct {
	Cookie    string     `json:"cookie"`
	FetchedAt time.Time  `json:"fetched_at"`
	SourceURL string     `json:"source_url"`
	Provider  string     `json:"provider"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

func SaveReese84Record(ctx context.Context, c cache.Cache, record CookieRecord) error {
	if c == nil {
		return errors.New("cache is required")
	}

	record.Cookie = strings.TrimSpace(record.Cookie)
	if record.Cookie == "" {
		return errors.New("cookie is required")
	}
	// other fields are optional for now

	body, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("marshal reese84 record: %w", err)
	}

	// want to have fall backs
	historyKey := path.Join(Reese84HistoryPrefix, record.FetchedAt.Format(time.RFC3339Nano)+".json")
	if err := c.Put(ctx, historyKey, string(body), cache.Unconditional()); err != nil {
		return fmt.Errorf("write reese84 history: %w", err)
	}
	if err := c.Put(ctx, Reese84LatestCacheKey, string(body), cache.Unconditional()); err != nil {
		return fmt.Errorf("write reese84 latest: %w", err)
	}
	return nil
}

func LoadLatestReese84(ctx context.Context, c cache.Cache) (*CookieRecord, error) {
	if c == nil {
		return nil, errors.New("cache is required")
	}

	reader, err := c.Get(ctx, Reese84LatestCacheKey)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = reader.Close()
	}()

	var record CookieRecord
	if err := json.NewDecoder(reader).Decode(&record); err != nil {
		return nil, fmt.Errorf("decode reese84 record: %w", err)
	}
	record.Cookie = strings.TrimSpace(record.Cookie)
	if record.Cookie == "" {
		return nil, fmt.Errorf("decode reese84 record: cookie is empty")
	}
	return &record, nil
}

type CachedReese84Source struct {
	c cache.Cache
}

func NewCachedReese84Source(c cache.Cache) *CachedReese84Source {
	return &CachedReese84Source{
		c: c,
	}
}

func (s *CachedReese84Source) Value(ctx context.Context) (string, error) {
	cookie, err := LoadLatestReese84(ctx, s.c)
	if err != nil {
		slog.WarnContext(ctx, "failed to load cached albertsons reese84, using fallback cookie", "error", err)
		return "", err
	}

	return cookie.Cookie, nil
}
