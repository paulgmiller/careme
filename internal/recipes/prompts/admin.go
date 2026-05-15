package prompts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/recipes"
)

func AdminMenuPromptJSON(c cache.Cache) http.Handler {
	return adminPromptJSON(c, "menu", func(r *http.Request) (string, error) {
		hash := r.PathValue("hash")

		list, err := recipes.IO(c).FromCache(r.Context(), hash)
		if err != nil {
			return "", fmt.Errorf("load menu: %w", err)
		}
		if list.Plan == nil || strings.TrimSpace(list.Plan.ResponseID) == "" {
			return "", fmt.Errorf("menu prompt response id not found %w", cache.ErrNotFound)
		}
		return list.Plan.ResponseID, nil
	})
}

func AdminRecipePromptJSON(c cache.Cache) http.Handler {
	return adminPromptJSON(c, "recipe", func(r *http.Request) (string, error) {
		hash := r.PathValue("hash")

		recipe, err := recipes.IO(c).SingleFromCache(r.Context(), hash)
		if err != nil {
			return "", fmt.Errorf("load recipe: %w", err)
		}
		if strings.TrimSpace(recipe.ResponseID) == "" {
			return "", fmt.Errorf("recipe prompt response id not found %w", cache.ErrNotFound)
		}
		return recipe.ResponseID, nil
	})
}

func adminPromptJSON(c cache.Cache, kind string, responseIDFromRequest func(*http.Request) (string, error)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		responseID, err := responseIDFromRequest(r)
		if err != nil {
			if errors.Is(err, cache.ErrNotFound) {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}
			slog.ErrorContext(r.Context(), "failed to resolve prompt response id for admin json", "kind", kind, "hash", r.PathValue("hash"), "error", err)
			http.Error(w, "failed to resolve prompt response id", http.StatusInternalServerError)
			return
		}

		record, err := promptRecordWithParentInputsFromCache(r.Context(), c, responseID)
		if err != nil {
			if errors.Is(err, cache.ErrNotFound) {
				http.Error(w, "prompt not found in cache", http.StatusNotFound)
				return
			}
			slog.ErrorContext(r.Context(), "failed to load prompt for admin json", "kind", kind, "hash", r.PathValue("hash"), "response_id", responseID, "error", err)
			http.Error(w, "failed to load prompt", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		if err := enc.Encode(record); err != nil {
			slog.ErrorContext(r.Context(), "failed to encode prompt for admin json", "kind", kind, "hash", r.PathValue("hash"), "response_id", responseID, "error", err)
		}
	})
}

func promptRecordWithParentInputsFromCache(ctx context.Context, c cache.Cache, responseID string) (*ai.PromptRecord, error) {
	responseID = strings.TrimSpace(responseID)
	if responseID == "" {
		return nil, cache.ErrNotFound
	}

	record, err := promptRecordFromCache(ctx, c, responseID)
	if err != nil {
		return nil, err
	}

	parentResponseID := strings.TrimSpace(record.PreviousResponseID)
	if parentResponseID == "" {
		record.Input = append([]ai.PromptMessage(nil), record.Input...)
		return record, nil
	}

	parent, err := promptRecordWithParentInputsFromCache(ctx, c, parentResponseID)
	if err != nil {
		return nil, err
	}
	mergedInput := make([]ai.PromptMessage, 0, len(parent.Input)+len(record.Input))
	mergedInput = append(mergedInput, parent.Input...)
	mergedInput = append(mergedInput, record.Input...)
	record.Input = mergedInput
	return record, nil
}

func promptRecordFromCache(ctx context.Context, c cache.Cache, responseID string) (*ai.PromptRecord, error) {
	promptReader, err := c.Get(ctx, CachePrefix+responseID)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := promptReader.Close(); err != nil {
			slog.ErrorContext(ctx, "failed to close cached prompt reader", "response_id", responseID, "error", err)
		}
	}()

	var record ai.PromptRecord
	if err := json.NewDecoder(promptReader).Decode(&record); err != nil {
		return nil, fmt.Errorf("failed to decode prompt record: %w", err)
	}
	return &record, nil
}
