package feedback

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"careme/internal/cache"
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
