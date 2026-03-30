package albertsons

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path"
	"strings"
	"sync"
	"time"

	"careme/internal/cache"
)

const (
	Reese84LatestCacheKey   = "albertsons/reese84/latest.json"
	Reese84HistoryPrefix    = "albertsons/reese84/history/"
	DefaultReese84MaxAge    = 6 * time.Hour
	brightDataBrowserSource = "brightdata-browser-api"
)

type Reese84Record struct {
	Cookie    string     `json:"cookie"`
	FetchedAt time.Time  `json:"fetched_at"`
	SourceURL string     `json:"source_url"`
	Provider  string     `json:"provider"`
	TTLHours  int        `json:"ttl_hours"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

func SaveReese84Record(ctx context.Context, c cache.Cache, record Reese84Record) error {
	if c == nil {
		return errors.New("cache is required")
	}

	record.Cookie = strings.TrimSpace(record.Cookie)
	if record.Cookie == "" {
		return errors.New("cookie is required")
	}
	if record.FetchedAt.IsZero() {
		record.FetchedAt = time.Now().UTC()
	} else {
		record.FetchedAt = record.FetchedAt.UTC()
	}
	record.SourceURL = strings.TrimSpace(record.SourceURL)
	record.Provider = strings.TrimSpace(record.Provider)
	if record.Provider == "" {
		record.Provider = brightDataBrowserSource
	}
	if record.TTLHours <= 0 {
		record.TTLHours = int(DefaultReese84MaxAge / time.Hour)
	}
	if record.ExpiresAt != nil {
		expires := record.ExpiresAt.UTC()
		record.ExpiresAt = &expires
	}

	body, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("marshal reese84 record: %w", err)
	}

	historyKey := path.Join(Reese84HistoryPrefix, record.FetchedAt.Format(time.RFC3339Nano)+".json")
	if err := c.Put(ctx, historyKey, string(body), cache.Unconditional()); err != nil {
		return fmt.Errorf("write reese84 history: %w", err)
	}
	if err := c.Put(ctx, Reese84LatestCacheKey, string(body), cache.Unconditional()); err != nil {
		return fmt.Errorf("write reese84 latest: %w", err)
	}
	return nil
}

func LoadLatestReese84(ctx context.Context, c cache.Cache) (*Reese84Record, error) {
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

	var record Reese84Record
	if err := json.NewDecoder(reader).Decode(&record); err != nil {
		return nil, fmt.Errorf("decode reese84 record: %w", err)
	}
	record.Cookie = strings.TrimSpace(record.Cookie)
	if record.Cookie == "" {
		return nil, fmt.Errorf("decode reese84 record: cookie is empty")
	}
	return &record, nil
}

func LoadFreshReese84(ctx context.Context, c cache.Cache, maxAge time.Duration, now time.Time) (string, error) {
	record, err := LoadLatestReese84(ctx, c)
	if err != nil {
		return "", err
	}

	if maxAge > 0 {
		if now.IsZero() {
			now = time.Now().UTC()
		}
		if record.FetchedAt.IsZero() {
			return "", cache.ErrNotFound
		}
		if now.UTC().Sub(record.FetchedAt.UTC()) > maxAge {
			return "", cache.ErrNotFound
		}
	}
	return record.Cookie, nil
}

type CachedReese84Source struct {
	fallback     string
	maxAge       time.Duration
	cacheFactory func() (cache.Cache, error)

	once  sync.Once
	cache cache.Cache
	err   error
}

func NewCachedReese84Source(fallback string, maxAge time.Duration, cacheFactory func() (cache.Cache, error)) *CachedReese84Source {
	if maxAge <= 0 {
		maxAge = DefaultReese84MaxAge
	}
	return &CachedReese84Source{
		fallback:     strings.TrimSpace(fallback),
		maxAge:       maxAge,
		cacheFactory: cacheFactory,
	}
}

func (s *CachedReese84Source) Value(ctx context.Context) (string, error) {
	cacheStore, err := s.cacheStore()
	if err != nil {
		if s.fallback != "" {
			slog.Warn("failed to initialize albertsons reese84 cache, using fallback cookie", "error", err)
			return s.fallback, nil
		}
		return "", err
	}

	cookie, err := LoadFreshReese84(ctx, cacheStore, s.maxAge, time.Time{})
	if err == nil {
		return cookie, nil
	}
	if errors.Is(err, cache.ErrNotFound) {
		return s.fallback, nil
	}
	if s.fallback != "" {
		slog.WarnContext(ctx, "failed to load cached albertsons reese84, using fallback cookie", "error", err)
		return s.fallback, nil
	}
	return "", err
}

func (s *CachedReese84Source) cacheStore() (cache.Cache, error) {
	s.once.Do(func() {
		if s.cacheFactory == nil {
			s.err = errors.New("cache factory is required")
			return
		}
		s.cache, s.err = s.cacheFactory()
	})
	return s.cache, s.err
}
