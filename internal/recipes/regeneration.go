package recipes

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"log/slog"
	"strings"
	"time"

	"careme/internal/cache"

	"github.com/samber/lo"
)

const recipeRegenerationJobPrefix = "recipe_regenerations/"

type recipeRegenerationState string

const (
	recipeRegenerationRunning  recipeRegenerationState = "running"
	recipeRegenerationComplete recipeRegenerationState = "complete"
)

type recipeRegenerationJob struct {
	State     recipeRegenerationState `json:"state"`
	NewHash   string                  `json:"new_hash,omitempty"`
	UpdatedAt time.Time               `json:"updated_at"`
}

func recipeRegenerationJobID(oldHash, responseID string) string {
	h := fnv.New128a()
	_ = lo.Must(io.WriteString(h, strings.TrimSpace(oldHash)))
	_ = lo.Must(io.WriteString(h, strings.TrimSpace(responseID)))
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}

func (rio recipeio) startRecipeRegenerationJob(ctx context.Context, id string, opts cache.PutOptions) error {
	if !validRecipeRegenerationJobID(id) {
		return fmt.Errorf("invalid recipe regeneration job id")
	}
	job := recipeRegenerationJob{
		State:     recipeRegenerationRunning,
		UpdatedAt: time.Now().UTC(),
	}

	raw, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("marshal recipe regeneration job: %w", err)
	}
	if err := rio.Cache.Put(ctx, recipeRegenerationJobKey(id), string(raw), opts); err != nil {
		return fmt.Errorf("start recipe regeneration job: %w", err)
	}
	return nil
}

func (rio recipeio) completeRecipeRegenerationJob(ctx context.Context, id, hash string) error {
	if !validRecipeRegenerationJobID(id) {
		return fmt.Errorf("valid recipe regeneration job id is required")
	}

	job := recipeRegenerationJob{
		State:     recipeRegenerationComplete,
		NewHash:   hash,
		UpdatedAt: time.Now().UTC(),
	}
	raw, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("marshal recipe regeneration job: %w", err)
	}
	if err := rio.Cache.Put(ctx, recipeRegenerationJobKey(id), string(raw), cache.Unconditional()); err != nil {
		return fmt.Errorf("save recipe regeneration job: %w", err)
	}
	return nil
}

func (rio recipeio) loadRecipeRegenerationJob(ctx context.Context, id string) (recipeRegenerationJob, error) {
	id = strings.TrimSpace(id)
	if !validRecipeRegenerationJobID(id) {
		return recipeRegenerationJob{}, cache.ErrNotFound
	}
	r, err := rio.Cache.Get(ctx, recipeRegenerationJobKey(id))
	if err != nil {
		return recipeRegenerationJob{}, err
	}
	defer func() {
		if err := r.Close(); err != nil {
			slog.ErrorContext(ctx, "failed to close recipe regeneration job", "job_id", id, "error", err)
		}
	}()
	var job recipeRegenerationJob
	if err := json.NewDecoder(r).Decode(&job); err != nil {
		return recipeRegenerationJob{}, fmt.Errorf("decode recipe regeneration job: %w", err)
	}
	return job, nil
}

func recipeRegenerationJobKey(id string) string {
	return recipeRegenerationJobPrefix + strings.TrimSpace(id)
}

func validRecipeRegenerationJobID(id string) bool {
	raw, err := base64.RawURLEncoding.DecodeString(id)
	return err == nil && len(raw) == 16 && base64.RawURLEncoding.EncodeToString(raw) == id
}
