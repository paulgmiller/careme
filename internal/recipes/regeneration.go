package recipes

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"log/slog"
	"strings"
	"time"

	"careme/internal/cache"
)

const recipeRegenerationJobPrefix = "recipe_regenerations/"

type recipeRegenerationState string

const (
	recipeRegenerationRunning  recipeRegenerationState = "running"
	recipeRegenerationComplete recipeRegenerationState = "complete"
	recipeRegenerationFailed   recipeRegenerationState = "failed"
)

type recipeRegenerationJob struct {
	ID         string                  `json:"id"`
	OldHash    string                  `json:"old_hash"`
	ResponseID string                  `json:"response_id"`
	Attempt    int                     `json:"attempt"`
	State      recipeRegenerationState `json:"state"`
	NewHash    string                  `json:"new_hash,omitempty"`
	CreatedAt  time.Time               `json:"created_at"`
	UpdatedAt  time.Time               `json:"updated_at"`
}

func recipeRegenerationJobID(oldHash, responseID string, attempt int) string {
	h := fnv.New128a()
	_, _ = io.WriteString(h, strings.TrimSpace(oldHash))
	_, _ = h.Write([]byte{0})
	_, _ = io.WriteString(h, strings.TrimSpace(responseID))
	_, _ = h.Write([]byte{0, byte(attempt >> 24), byte(attempt >> 16), byte(attempt >> 8), byte(attempt)})
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}

func newRecipeRegenerationJob(oldHash, responseID string) recipeRegenerationJob {
	return newRecipeRegenerationAttempt(oldHash, responseID, 1)
}

func newRecipeRegenerationAttempt(oldHash, responseID string, attempt int) recipeRegenerationJob {
	now := time.Now().UTC()
	return recipeRegenerationJob{
		ID:         recipeRegenerationJobID(oldHash, responseID, attempt),
		OldHash:    strings.TrimSpace(oldHash),
		ResponseID: strings.TrimSpace(responseID),
		Attempt:    attempt,
		State:      recipeRegenerationRunning,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
}

func (rio recipeio) createRecipeRegenerationJob(ctx context.Context, job recipeRegenerationJob) (bool, error) {
	job.ID = strings.TrimSpace(job.ID)
	job.OldHash = strings.TrimSpace(job.OldHash)
	job.ResponseID = strings.TrimSpace(job.ResponseID)
	if job.ID == "" || job.OldHash == "" || job.ResponseID == "" || job.Attempt < 1 {
		return false, fmt.Errorf("recipe regeneration job requires id, old hash, response id, and attempt")
	}
	if !validRecipeRegenerationJobID(job.ID) || job.ID != recipeRegenerationJobID(job.OldHash, job.ResponseID, job.Attempt) {
		return false, fmt.Errorf("invalid recipe regeneration job id")
	}
	now := time.Now().UTC()
	if job.State == "" {
		job.State = recipeRegenerationRunning
	}
	if job.CreatedAt.IsZero() {
		job.CreatedAt = now
	}
	job.UpdatedAt = now
	raw, err := json.Marshal(job)
	if err != nil {
		return false, fmt.Errorf("marshal recipe regeneration job: %w", err)
	}
	if err := rio.Cache.Put(ctx, recipeRegenerationJobKey(job.ID), string(raw), cache.IfNoneMatch()); err != nil {
		if errors.Is(err, cache.ErrAlreadyExists) {
			return false, nil
		}
		return false, fmt.Errorf("create recipe regeneration job: %w", err)
	}
	return true, nil
}

func (rio recipeio) saveRecipeRegenerationJob(ctx context.Context, job recipeRegenerationJob) error {
	job.ID = strings.TrimSpace(job.ID)
	if !validRecipeRegenerationJobID(job.ID) {
		return fmt.Errorf("valid recipe regeneration job id is required")
	}
	job.UpdatedAt = time.Now().UTC()
	raw, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("marshal recipe regeneration job: %w", err)
	}
	if err := rio.Cache.Put(ctx, recipeRegenerationJobKey(job.ID), string(raw), cache.Unconditional()); err != nil {
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
	return recipeRegenerationJobPrefix + strings.TrimSpace(id) + ".json"
}

func validRecipeRegenerationJobID(id string) bool {
	raw, err := base64.RawURLEncoding.DecodeString(id)
	return err == nil && len(raw) == 16 && base64.RawURLEncoding.EncodeToString(raw) == id
}
