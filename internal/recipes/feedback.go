package recipes

import (
	"careme/internal/cache"
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/samber/lo"
)

const recipeFeedbackPrefix = "recipe_feedback/"

type RecipeFeedback struct {
	Cooked    bool      `json:"cooked"`
	Stars     int       `json:"stars,omitempty"`
	Comment   string    `json:"comment,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (rio recipeio) FeedbackFromCache(ctx context.Context, hash string) (*RecipeFeedback, error) {
	feedbackBlob, err := rio.Cache.Get(ctx, recipeFeedbackPrefix+hash)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := feedbackBlob.Close(); err != nil {
			slog.ErrorContext(ctx, "failed to close cached recipe feedback", "hash", hash, "error", err)
		}
	}()

	var feedback RecipeFeedback
	if err := json.NewDecoder(feedbackBlob).Decode(&feedback); err != nil {
		return nil, err
	}
	return &feedback, nil
}

func (rio recipeio) SaveFeedback(ctx context.Context, hash string, feedback RecipeFeedback) error {
	feedbackJSON := lo.Must(json.Marshal(feedback))
	if err := rio.Cache.Put(ctx, recipeFeedbackPrefix+hash, string(feedbackJSON), cache.Unconditional()); err != nil {
		slog.ErrorContext(ctx, "failed to cache recipe feedback", "hash", hash, "error", err)
		return err
	}
	return nil
}
