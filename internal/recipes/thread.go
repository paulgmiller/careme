package recipes

import (
	"careme/internal/cache"
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/samber/lo"
)

const recipeThreadPrefix = "recipe_thread/"

type RecipeThreadEntry struct {
	Question  string    `json:"question"`
	Answer    string    `json:"answer"`
	CreatedAt time.Time `json:"created_at"`
}

func (rio recipeio) ThreadFromCache(ctx context.Context, hash string) ([]RecipeThreadEntry, error) {
	thread, err := rio.Cache.Get(ctx, recipeThreadPrefix+hash)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := thread.Close(); err != nil {
			slog.ErrorContext(ctx, "failed to close cached thread", "hash", hash, "error", err)
		}
	}()

	var entries []RecipeThreadEntry
	if err := json.NewDecoder(thread).Decode(&entries); err != nil {
		return nil, err
	}
	return entries, nil
}

func (rio recipeio) SaveThread(ctx context.Context, hash string, entries []RecipeThreadEntry) error {
	threadJSON := lo.Must(json.Marshal(entries))
	if err := rio.Cache.Put(ctx, recipeThreadPrefix+hash, string(threadJSON), cache.Unconditional()); err != nil {
		slog.ErrorContext(ctx, "failed to cache recipe thread", "hash", hash, "error", err)
		return err
	}
	return nil
}
