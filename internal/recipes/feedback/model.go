package feedback

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"careme/internal/cache"

	"github.com/samber/lo"
	lop "github.com/samber/lo/parallel"
)

type Feedback struct {
	Cooked    bool      `json:"cooked"`
	Stars     int       `json:"stars,omitempty"`
	Comment   string    `json:"comment,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

type FeedbackIO struct {
	c cache.Cache
}

type feedbackResult struct {
	Hash     string
	Feedback Feedback
}

func NewIO(c cache.Cache) FeedbackIO {
	if c == nil {
		panic("cache cannot be nil")
	}
	return FeedbackIO{c: c}
}

const recipeFeedbackPrefix = "recipe_feedback/"

func (fio FeedbackIO) FeedbackFromCache(ctx context.Context, hash string) (*Feedback, error) {
	feedbackBlob, err := fio.c.Get(ctx, recipeFeedbackPrefix+hash)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := feedbackBlob.Close(); err != nil {
			slog.ErrorContext(ctx, "failed to close cached recipe feedback", "hash", hash, "error", err)
		}
	}()

	var state Feedback
	if err := json.NewDecoder(feedbackBlob).Decode(&state); err != nil {
		return nil, err
	}
	return &state, nil
}

func (fio FeedbackIO) SaveFeedback(ctx context.Context, hash string, feedback Feedback) error {
	feedbackJSON, err := json.Marshal(feedback)
	if err != nil {
		return err
	}
	if err := fio.c.Put(ctx, recipeFeedbackPrefix+hash, string(feedbackJSON), cache.Unconditional()); err != nil {
		slog.ErrorContext(ctx, "failed to cache recipe feedback", "hash", hash, "error", err)
		return err
	}
	return nil
}

func (fio FeedbackIO) FeedbackByHash(ctx context.Context, hashes []string) map[string]Feedback {
	results := lop.Map(hashes, func(hash string, _ int) feedbackResult {
		state, err := fio.FeedbackFromCache(ctx, hash)
		if err != nil {
			if !errors.Is(err, cache.ErrNotFound) {
				slog.WarnContext(ctx, "failed to load recipe feedback", "hash", hash, "error", err)
			}
			return feedbackResult{}
		}
		return feedbackResult{
			Hash:     hash,
			Feedback: *state,
		}
	})
	results = lo.Compact(results)
	return lo.SliceToMap(results, func(result feedbackResult) (string, Feedback) {
		return result.Hash, result.Feedback
	})
}
