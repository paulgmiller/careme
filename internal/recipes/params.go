package recipes

import (
	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/locations"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"time"

	"github.com/samber/lo"
)

type generatorParams struct {
	Location *locations.Location `json:"location,omitempty"`
	Date     time.Time           `json:"date,omitempty"`
	Staples  []filter            `json:"staples,omitempty"`
	// People       int
	Instructions   string      `json:"instructions,omitempty"`
	LastRecipes    []string    `json:"last_recipes,omitempty"`
	UserID         string      `json:"user_id,omitempty"`
	ConversationID string      `json:"conversation_id,omitempty"` // Can remove if we pass it in seperately to generate recipes?
	Saved          []ai.Recipe `json:"saved_recipes,omitempty"`
	Dismissed      []ai.Recipe `json:"dismissed_recipes,omitempty"`
}

func DefaultParams(l *locations.Location, date time.Time) *generatorParams {
	// normalize to midnight (shave hours, minutes, seconds, nanoseconds)
	date = time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, date.Location())
	return &generatorParams{
		Date:     date, // shave time
		Location: l,
		// People:   2,
		Staples: DefaultStaples(),
	}
}

func (g *generatorParams) String() string {
	return fmt.Sprintf("%s on %s", g.Location.ID, g.Date.Format("2006-01-02"))
}

// Hash this is how we find shoppinglists and params
// intentionally not including ConversationID to preserve old hashes
func (g *generatorParams) Hash() string {
	fnv := fnv.New64a()
	lo.Must(io.WriteString(fnv, g.Location.ID))
	lo.Must(io.WriteString(fnv, g.Date.Format("2006-01-02")))
	bytes := lo.Must(json.Marshal(g.Staples))
	lo.Must(fnv.Write(bytes))
	lo.Must(io.WriteString(fnv, g.Instructions)) // rethink this? if they're all in convo should we have one id and ability to walk back?
	for _, saved := range g.Saved {
		lo.Must(io.WriteString(fnv, "saved"+saved.ComputeHash()))
	}
	for _, dismissed := range g.Dismissed {
		lo.Must(io.WriteString(fnv, "dismissed"+dismissed.ComputeHash()))
	}
	// this is actually a list not a recipe and isn't necessary. TODO figure out how to remove
	// could fix without breaking by doing two lookups?
	return base64.URLEncoding.EncodeToString(fnv.Sum([]byte("recipe")))
}

// so far just excludes instructions. Can exclude people and other things
func (g *generatorParams) LocationHash() string {
	fnv := fnv.New64a()
	lo.Must(io.WriteString(fnv, g.Location.ID))
	lo.Must(io.WriteString(fnv, g.Date.Format("2006-01-02")))
	bytes := lo.Must(json.Marshal(g.Staples)) // excited fro this to break in some wierd way
	lo.Must(fnv.Write(bytes))
	// see comment above this suffix is unceessary but keeps old hashes working
	return base64.URLEncoding.EncodeToString(fnv.Sum([]byte("ingredients")))
}

// loadParamsFromHash loads generator params from cache using the hash
func loadParamsFromHash(ctx context.Context, hash string, c cache.Cache) (*generatorParams, error) {
	paramsReader, err := c.Get(ctx, hash+".params")
	if err != nil {
		return nil, fmt.Errorf("params not found for hash %s: %w", hash, err)
	}
	defer paramsReader.Close()

	var params generatorParams
	if err := json.NewDecoder(paramsReader).Decode(&params); err != nil {
		return nil, fmt.Errorf("failed to decode params: %w", err)
	}
	return &params, nil
}

func DefaultStaples() []filter {
	return []filter{
		{
			Term:   "beef",
			Brands: []string{"Simple Truth", "Kroger"},
		},
		{
			Term:   "chicken",
			Brands: []string{"Foster Farms", "Draper Valley", "Simple Truth"}, //"Simple Truth"? do these vary in every state?
		},
		{
			Term: "fish",
		},
		{
			Term:   "pork", // Kroger?
			Brands: []string{"PORK", "Kroger", "Harris Teeter"},
		},
		{
			Term:   "shellfish",
			Brands: []string{"Sand Bar", "Kroger"},
			Frozen: true, // remove after 500 sadness?
		},
		{
			Term:   "lamb",
			Brands: []string{"Simple Truth"},
		},
		{
			Term:   "produce vegetable",
			Brands: []string{"*"}, // ther's alot of fresh * and kroger here. cut this down after 500 sadness
		},
	}
}
