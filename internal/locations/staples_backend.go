package locations

import (
	"context"

	"careme/internal/ai"
)

// StaplesBackend is the provider contract consumed by recipe sourcing.
type StaplesBackend interface {
	IsID(string) bool
	Signature() string
	FetchStaples(context.Context, string) ([]ai.InputIngredient, error)
	FetchWines(context.Context, string, []string) ([]ai.InputIngredient, error)
}
