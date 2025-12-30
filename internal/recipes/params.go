package recipes

import (
	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/locations"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/samber/lo"
)

type generatorParams struct {
	Location *locations.Location `json:"location,omitempty"`
	Date     time.Time           `json:"date,omitempty"`
	Staples  []filter            `json:"staples,omitempty"`
	// People       int
	Instructions string   `json:"instructions,omitempty"`
	LastRecipes  []string `json:"last_recipes,omitempty"`
	// UserID         string      `json:"user_id,omitempty"`
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

func (s *server) ParseQueryArgs(ctx context.Context, r *http.Request) (*generatorParams, error) {
	loc := r.URL.Query().Get("location")
	if loc == "" {
		return nil, errors.New("must provide location id")
	}

	l, err := s.locServer.GetLocationByID(ctx, loc)
	if err != nil {
		return nil, err
	}

	dateStr := r.URL.Query().Get("date")
	if dateStr == "" {
		dateStr = time.Now().Format("2006-01-02")
	}
	date, err := time.ParseInLocation("2006-01-02", dateStr, time.UTC)
	if err != nil {
		return nil, err
	}

	p := DefaultParams(l, date)
	p.Instructions = r.URL.Query().Get("instructions")

	// Handle saved and dismissed recipe hashes from checkboxes
	// Query().Get returns first value, Query() returns all values
	// will be empty values for every recipe and two for ones with no action
	// TODO look at way not to duplicate so many query arguments and pass down just a saved list or a query arg for each saved item.
	clean := func(s string, _ int) (string, bool) {
		ts := strings.TrimSpace(s)
		return ts, ts != ""
	}
	savedHashes := lo.FilterMap(r.URL.Query()["saved"], clean)
	dismissedHashes := lo.FilterMap(r.URL.Query()["dismissed"], clean)
	// Load saved recipes from cache by their hashes
	for _, hash := range savedHashes {
		recipe, err := s.SingleFromCache(ctx, hash)
		if err != nil {
			slog.ErrorContext(ctx, "failed to load saved recipe by hash", "hash", hash, "error", err)
			continue
		}
		slog.InfoContext(ctx, "adding saved recipe to params", "title", recipe.Title, "hash", hash)
		p.Saved = append(p.Saved, *recipe)
	}

	// Add dismissed recipe titles to instructions so AI knows what to avoid
	for _, hash := range dismissedHashes {
		recipe, err := s.SingleFromCache(ctx, hash)
		if err != nil {
			slog.ErrorContext(ctx, "failed to load dismissed recipe by hash", "hash", hash, "error", err)
			continue
		}
		slog.InfoContext(ctx, "adding dismissed recipe to params", "title", recipe.Title, "hash", hash)
		p.Dismissed = append(p.Dismissed, *recipe)
	}
	// should this be in hash?
	p.ConversationID = strings.TrimSpace(r.URL.Query().Get("conversation_id"))

	return p, nil
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
