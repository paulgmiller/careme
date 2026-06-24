package farmersmarket

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"careme/internal/cache"
)

const analysisJobPrefix = "analysis_jobs/"

type analysisState string

const (
	analysisStateRunning  analysisState = "running"
	analysisStateComplete analysisState = "complete"
	analysisStateFailed   analysisState = "failed"
)

type analysisStatus struct {
	ID              string        `json:"id"`
	UserID          string        `json:"user_id"`
	State           analysisState `json:"state"`
	PhotoCount      int           `json:"photo_count"`
	PhotosAnalyzed  int           `json:"photos_analyzed"`
	IngredientCount int           `json:"ingredient_count"`
	Message         string        `json:"message"`
	RedirectURL     string        `json:"redirect_url,omitempty"`
	Error           string        `json:"error,omitempty"`
	CreatedAt       time.Time     `json:"created_at"`
	UpdatedAt       time.Time     `json:"updated_at"`
}

type analysisStatusStore struct {
	cache cache.Cache
}

func newAnalysisStatusStore(c cache.Cache) *analysisStatusStore {
	return &analysisStatusStore{cache: c}
}

func newAnalysisJobID() (string, error) {
	var b [18]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("create analysis job id: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

func (s *analysisStatusStore) save(ctx context.Context, status analysisStatus) error {
	status.ID = strings.TrimSpace(status.ID)
	if status.ID == "" {
		return fmt.Errorf("analysis job id is required")
	}
	now := time.Now().UTC()
	if status.CreatedAt.IsZero() {
		status.CreatedAt = now
	}
	status.UpdatedAt = now
	raw, err := json.Marshal(status)
	if err != nil {
		return fmt.Errorf("marshal farmers market analysis status: %w", err)
	}
	if err := s.cache.Put(ctx, analysisJobKey(status.ID), string(raw), cache.Unconditional()); err != nil {
		return fmt.Errorf("save farmers market analysis status: %w", err)
	}
	return nil
}

func (s *analysisStatusStore) load(ctx context.Context, id string) (analysisStatus, error) {
	reader, err := s.cache.Get(ctx, analysisJobKey(id))
	if err != nil {
		return analysisStatus{}, err
	}
	defer func() {
		if err := reader.Close(); err != nil {
			slog.ErrorContext(ctx, "failed to close farmers market analysis status", "id", id, "error", err)
		}
	}()
	raw, err := io.ReadAll(reader)
	if err != nil {
		return analysisStatus{}, fmt.Errorf("read farmers market analysis status: %w", err)
	}
	var status analysisStatus
	if err := json.Unmarshal(raw, &status); err != nil {
		return analysisStatus{}, fmt.Errorf("decode farmers market analysis status: %w", err)
	}
	return status, nil
}

func analysisJobKey(id string) string {
	return analysisJobPrefix + strings.TrimSpace(id) + ".json"
}
