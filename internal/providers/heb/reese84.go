package heb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"

	"careme/internal/cache"
)

const (
	Reese84LatestCacheKey = "heb/reese84/latest.json"
	Reese84HistoryPrefix  = "heb/reese84/history/"
)

type Reese84Record struct {
	Cookie    string     `json:"cookie"`
	FetchedAt time.Time  `json:"fetched_at"`
	SourceURL string     `json:"source_url"`
	Provider  string     `json:"provider"`
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

	body, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("marshal heb reese84 record: %w", err)
	}

	historyKey := path.Join(Reese84HistoryPrefix, record.FetchedAt.Format(time.RFC3339Nano)+".json")
	if err := c.Put(ctx, historyKey, string(body), cache.Unconditional()); err != nil {
		return fmt.Errorf("write heb reese84 history: %w", err)
	}
	if err := c.Put(ctx, Reese84LatestCacheKey, string(body), cache.Unconditional()); err != nil {
		return fmt.Errorf("write heb reese84 latest: %w", err)
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
		return nil, fmt.Errorf("decode heb reese84 record: %w", err)
	}
	record.Cookie = strings.TrimSpace(record.Cookie)
	if record.Cookie == "" {
		return nil, fmt.Errorf("decode heb reese84 record: cookie is empty")
	}
	return &record, nil
}
