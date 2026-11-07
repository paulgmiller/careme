package status

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"
)

const slotDecisionCachePrefix = "generation_slot_decision/"

var (
	ErrSlotCompleted = errors.New("meal slot is already completed")
	ErrInvalidSlot   = errors.New("invalid meal slot or revision")
)

type slotDecision struct {
	Kind       string `json:"kind"`
	RecipeHash string `json:"recipe_hash,omitempty"`
}

func decisionKey(hash string, startedAt time.Time, index, revision int) (string, error) {
	if strings.TrimSpace(hash) == "" || index < 0 || revision < 0 {
		return "", ErrInvalidSlot
	}
	return fmt.Sprintf("%s%s/%d/%d/%d", slotDecisionCachePrefix, hash, startedAt.UnixNano(), index, revision), nil
}

func (ss *Store) loadDecision(ctx context.Context, hash string, startedAt time.Time, index, revision int) (slotDecision, error) {
	key, err := decisionKey(hash, startedAt, index, revision)
	if err != nil {
		return slotDecision{}, err
	}
	reader, err := ss.cache.Get(ctx, key)
	if errors.Is(err, cache.ErrNotFound) {
		return slotDecision{}, nil
	}
	if err != nil {
		return slotDecision{}, fmt.Errorf("get slot decision: %w", err)
	}
	defer func() {
		if err := reader.Close(); err != nil {
			slog.ErrorContext(ctx, "failed to close slot decision reader", "hash", hash, "index", index, "revision", revision, "error", err)
		}
	}()
	var decision slotDecision
	if err := json.NewDecoder(reader).Decode(&decision); err != nil {
		return slotDecision{}, fmt.Errorf("decode slot decision: %w", err)
	}
	if decision.Kind != "replace" && (decision.Kind != "ready" || decision.RecipeHash == "") {
		return slotDecision{}, fmt.Errorf("invalid slot decision for %s", key)
	}
	return decision, nil
}

// Decision reports whether replacement won this revision's immutable decision.
func (ss *Store) Decision(ctx context.Context, hash string, index, revision int) (bool, error) {
	stored, err := ss.load(ctx, hash)
	if err != nil {
		return false, err
	}
	decision, err := ss.loadDecision(ctx, hash, stored.StartedAt, index, revision)
	return decision.Kind == "replace", err
}

func (ss *Store) decide(ctx context.Context, hash string, startedAt time.Time, index, revision int, proposed slotDecision) (slotDecision, error) {
	key, err := decisionKey(hash, startedAt, index, revision)
	if err != nil {
		return slotDecision{}, err
	}
	raw, err := json.Marshal(proposed)
	if err != nil {
		return slotDecision{}, fmt.Errorf("marshal slot decision: %w", err)
	}
	err = ss.cache.Put(ctx, key, string(raw), cache.IfNoneMatch())
	if errors.Is(err, cache.ErrAlreadyExists) {
		return ss.loadDecision(ctx, hash, startedAt, index, revision)
	}
	if err != nil {
		return slotDecision{}, fmt.Errorf("save slot decision: %w", err)
	}
	return proposed, nil
}

func (ss *Store) runningSlot(ctx context.Context, hash string, index, revision int) (payload, error) {
	stored, err := ss.load(ctx, hash)
	if err != nil {
		return payload{}, err
	}
	if stored.Redirect != "" {
		return payload{}, ErrSlotCompleted
	}
	if failed := stored.failed(); failed != "" {
		return payload{}, fmt.Errorf("generation %s is no longer running: %s", hash, failed)
	}
	if index < 0 || index >= len(stored.Slots) || revision < 0 || stored.Slots[index].Revision != revision {
		return payload{}, ErrInvalidSlot
	}
	return stored, nil
}

// RequestReplacement can run on any replica. It never writes the owner's status.
func (ss *Store) RequestReplacement(ctx context.Context, hash string, index, revision int) error {
	stored, err := ss.runningSlot(ctx, hash, index, revision)
	if err != nil {
		return err
	}
	if stored.Slots[index].Reviewed {
		return ErrSlotCompleted
	}
	decision, err := ss.decide(ctx, hash, stored.StartedAt, index, revision, slotDecision{Kind: "replace"})
	if err != nil {
		return err
	}
	if decision.Kind == "ready" {
		return ErrSlotCompleted
	}
	return nil
}

// FinishSlot competes with replacement without publishing the ready recipe.
func (ss *Store) FinishSlot(ctx context.Context, hash string, index, revision int, recipeHash string) (bool, error) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	if strings.TrimSpace(recipeHash) == "" {
		return false, fmt.Errorf("ready recipe hash is required")
	}
	stored, err := ss.runningSlot(ctx, hash, index, revision)
	if err != nil {
		return false, err
	}
	decision, err := ss.decide(ctx, hash, stored.StartedAt, index, revision, slotDecision{Kind: "ready", RecipeHash: recipeHash})
	if err != nil {
		return false, err
	}
	if decision.Kind == "ready" && decision.RecipeHash != recipeHash {
		return false, fmt.Errorf("slot %d revision %d already completed with another recipe", index, revision)
	}
	return decision.Kind == "ready", nil
}

// ReplaceSlot is called by the owner after joining the superseded worker.
func (ss *Store) ReplaceSlot(ctx context.Context, hash string, index, revision int, plan ai.RecipePlan) error {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	stored, err := ss.runningSlot(ctx, hash, index, revision-1)
	if err != nil {
		return err
	}
	decision, err := ss.loadDecision(ctx, hash, stored.StartedAt, index, revision-1)
	if err != nil {
		return err
	}
	if decision.Kind != "replace" {
		return fmt.Errorf("slot %d has no replacement request", index)
	}
	stored.Slots[index] = Slot{Plan: plan, Revision: revision}
	return ss.save(ctx, hash, stored)
}
