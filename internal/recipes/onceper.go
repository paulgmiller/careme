package recipes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"careme/internal/cache"
)

const (
	oncePerResultPrefix = "onceper/result/"
	oncePerClaimPrefix  = "onceper/claim/"
)

var ErrOncePerInProgress = errors.New("onceper check already in progress")

type oncePerResult struct {
	CheckedAt time.Time `json:"checked_at"`
	Error     string    `json:"error,omitempty"`
}

type OncePer struct {
	cache    cache.Cache
	name     string
	now      func() time.Time
	claimTTL time.Duration
}

func NewOncePer(c cache.Cache, name string) (OncePer, error) {
	if c == nil {
		return OncePer{}, errors.New("onceper cache is required")
	}
	if name == "" {
		return OncePer{}, errors.New("onceper name is required")
	}
	return OncePer{
		cache:    c,
		name:     name,
		now:      time.Now,
		claimTTL: 5 * time.Minute,
	}, nil
}

func (o OncePer) Do(ctx context.Context, period time.Duration, f func() error) error {
	if period <= 0 {
		return errors.New("onceper period must be positive")
	}

	for {
		now := o.now().UTC()
		resultKey := o.resultKey(period, now)
		result, err := o.loadResult(ctx, resultKey)
		if err == nil {
			return result.Err()
		}
		if err != nil && !errors.Is(err, cache.ErrNotFound) {
			return fmt.Errorf("read onceper result: %w", err)
		}

		claimUntil := now.Add(o.claimTTL)
		if err := o.tryClaim(ctx, period, now); err == nil {
			runErr := f()
			if writeErr := o.writeResult(ctx, resultKey, o.now().UTC(), runErr); writeErr != nil {
				if runErr != nil {
					return errors.Join(runErr, writeErr)
				}
				return writeErr
			}
			return runErr
		} else if !errors.Is(err, cache.ErrAlreadyExists) {
			return fmt.Errorf("write onceper claim: %w", err)
		}
		if o.now().UTC().Before(claimUntil) {
			return ErrOncePerInProgress
		}
		// claim is expired loop and try and claim again.
	}
}

func (o OncePer) loadResult(ctx context.Context, key string) (*oncePerResult, error) {
	reader, err := o.cache.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = reader.Close()
	}()

	var result oncePerResult
	if err := json.NewDecoder(reader).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode onceper result: %w", err)
	}
	return &result, nil
}

func (o OncePer) tryClaim(ctx context.Context, period time.Duration, now time.Time) error {
	key := o.claimKey(period, now)
	body, err := json.Marshal(struct {
		ClaimedAt time.Time `json:"claimed_at"`
	}{
		ClaimedAt: now,
	})
	if err != nil {
		return fmt.Errorf("marshal onceper claim: %w", err)
	}
	return o.cache.Put(ctx, key, string(body), cache.IfNoneMatch())
}

func (o OncePer) writeResult(ctx context.Context, resultKey string, checkedAt time.Time, runErr error) error {
	result := oncePerResult{CheckedAt: checkedAt}
	if runErr != nil {
		result.Error = runErr.Error()
	}

	body, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("marshal onceper result: %w", err)
	}
	if err := o.cache.Put(ctx, resultKey, string(body), cache.Unconditional()); err != nil {
		return fmt.Errorf("write onceper result: %w", err)
	}
	return nil
}

func (o OncePer) resultKey(period time.Duration, now time.Time) string {
	return o.resultKeyForSlot(period, oncePerSlot(period, now))
}

func (o OncePer) resultKeyForSlot(period time.Duration, slot int64) string {
	return fmt.Sprintf("%s%s/%d/%d.json", oncePerResultPrefix, o.name, period.Nanoseconds(), slot)
}

func (o OncePer) claimKey(period time.Duration, now time.Time) string {
	return fmt.Sprintf("%s%s/%d/%d/%d.json", oncePerClaimPrefix, o.name, period.Nanoseconds(), oncePerSlot(period, now), oncePerSlot(o.claimTTL, now))
}

func oncePerSlot(period time.Duration, now time.Time) int64 {
	return now.UnixNano() / period.Nanoseconds()
}

func (r *oncePerResult) Err() error {
	if r == nil || r.Error == "" {
		return nil
	}
	return errors.New(r.Error)
}
