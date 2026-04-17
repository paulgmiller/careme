package recipes

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"careme/internal/cache"
)

const generationStatusCachePrefix = "generation_status/"

const (
	generationStageStarting         = "starting"
	generationStageIngredientsReady = "ingredients_ready"
	generationStageRecipesGenerated = "recipes_generated"
	generationStageCritiqueRetrying = "critique_retrying"
	generationStageComplete         = "complete"
)

type GenerationStatus struct {
	Stage     string    `json:"stage"`
	Message   string    `json:"message"`
	UpdatedAt time.Time `json:"updated_at"`
}

type generationStatusStore interface {
	SaveGenerationStatus(ctx context.Context, hash string, status GenerationStatus) error
	GenerationStatusFromCache(ctx context.Context, hash string) (*GenerationStatus, error)
}

func newGenerationStatus(stage string) GenerationStatus {
	return GenerationStatus{
		Stage:     stage,
		Message:   generationStatusMessage(stage),
		UpdatedAt: time.Now().UTC(),
	}
}

func generationStatusMessage(stage string) string {
	switch stage {
	case generationStageStarting:
		return "Starting your recipe plan."
	case generationStageIngredientsReady:
		return "Ingredients are ready. Building your recipes."
	case generationStageRecipesGenerated:
		return "Recipes drafted. Giving them a quick quality check."
	case generationStageCritiqueRetrying:
		return "Checking the recipes and trying a better draft."
	case generationStageComplete:
		return "Recipes are ready."
	default:
		return "We're putting your recipes together."
	}
}

func (rio recipeio) GenerationStatusFromCache(ctx context.Context, hash string) (*GenerationStatus, error) {
	statusReader, err := rio.Cache.Get(ctx, generationStatusCachePrefix+hash)
	if err != nil {
		return nil, fmt.Errorf("error getting generation status for hash %s: %w", hash, err)
	}
	defer func() {
		if err := statusReader.Close(); err != nil {
			slog.ErrorContext(ctx, "failed to close generation status reader", "hash", hash, "error", err)
		}
	}()

	var status GenerationStatus
	if err := json.NewDecoder(statusReader).Decode(&status); err != nil {
		return nil, fmt.Errorf("failed to decode generation status for hash %s: %w", hash, err)
	}
	if status.Message == "" {
		status.Message = generationStatusMessage(status.Stage)
	}
	return &status, nil
}

func (rio recipeio) SaveGenerationStatus(ctx context.Context, hash string, status GenerationStatus) error {
	if status.Message == "" {
		status.Message = generationStatusMessage(status.Stage)
	}
	if status.UpdatedAt.IsZero() {
		status.UpdatedAt = time.Now().UTC()
	}
	body, err := json.Marshal(status)
	if err != nil {
		return err
	}
	return rio.Cache.Put(ctx, generationStatusCachePrefix+hash, string(body), cache.Unconditional())
}
