package ai

import (
	"context"
	"time"
)

type PromptMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type PromptRecord struct {
	CreatedAt          time.Time       `json:"created_at"`
	ResponseID         string          `json:"response_id"`
	Model              string          `json:"model"`
	Instructions       string          `json:"instructions,omitempty"`
	PreviousResponseID string          `json:"previous_response_id,omitempty"`
	Input              []PromptMessage `json:"input"`
}

type PromptRecorder interface {
	RecordPrompt(ctx context.Context, record *PromptRecord) error
}

type noopPromptRecorder struct{}

func (noopPromptRecorder) RecordPrompt(context.Context, *PromptRecord) error {
	return nil
}
