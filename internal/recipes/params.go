package recipes

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"net/http"
	"time"

	"careme/internal/ai"
	"careme/internal/locations"

	"github.com/samber/lo"
)

const (
	legacyRecipeHashSeed      = "recipe"
	legacyIngredientsHashSeed = "ingredients"
)

var nowFn = time.Now

type generatorParams struct {
	Location     *locations.Location `json:"location,omitempty"`
	Date         time.Time           `json:"date"`
	Instructions string              `json:"instructions,omitempty"`
	Directive    string              `json:"directive,omitempty"` // this is the new one that will be used. Can remove GenerationPrompt after a while.
	LastRecipes  []string            `json:"-"`                   // this doesn't get populated until after save.
	// UserID         string      `json:"user_id,omitempty"`
	// ideally this would be a section and we'd fetch titles and other things as needed
	// as is this records a selectio at the time of a regeneration
	Saved     []ai.Recipe `json:"saved_recipes,omitempty"`
	Dismissed []ai.Recipe `json:"dismissed_recipes,omitempty"`

	// regeneration-only context from the origin params; not hashed
	PriorSavedHashes               []string `json:"-"`
	PreviousMenuPlanResponseID     string   `json:"previous_menu_plan_response_id,omitempty"`
	PreviousMenuPlanPromptCacheKey string   `json:"previous_menu_plan_prompt_cache_key,omitempty"`
}

func (g *generatorParams) previousMenuPlanResponse() ai.ResponseRef {
	return ai.ResponseRef{
		ID:             g.PreviousMenuPlanResponseID,
		PromptCacheKey: g.PreviousMenuPlanPromptCacheKey,
	}
}

// exist for mail's interface be careful please.
type GeneratorParams = generatorParams

func DefaultParams(l *locations.Location, date time.Time) *generatorParams {
	// normalize to midnight (shave hours, minutes, seconds, nanoseconds)
	// rethink this can we use this to restart if we don't normalize and hash still just looks at right part?
	date = time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, date.Location())
	return &generatorParams{
		Date:     date, // shave time
		Location: l,
	}
}

// should go away if params gets its own pacakge? needed by produce score.
func ParamsLocationHash(l locations.Location, d time.Time) string {
	return DefaultParams(&l, d).LocationHash()
}

func (g *generatorParams) String() string {
	return fmt.Sprintf("%s on %s", g.Location.ID, g.Date.Format("2006-01-02"))
}

// Hash this is how we find shoppinglists and params
// intentionally not including ResponseID to preserve old hashes
func (g *generatorParams) Hash() string {
	fnv := fnv.New64a()
	lo.Must(io.WriteString(fnv, g.Location.ID))
	lo.Must(io.WriteString(fnv, g.Date.Format("2006-01-02")))
	lo.Must(io.WriteString(fnv, staplesSignatureForLocation(g.Location.ID)))
	lo.Must(io.WriteString(fnv, g.Instructions)) // rethink this? if they're all in convo should we have one id and ability to walk back?
	lo.Must(io.WriteString(fnv, g.Directive))
	for _, saved := range g.Saved {
		lo.Must(io.WriteString(fnv, "saved"+saved.ComputeHash()))
	}
	for _, dismissed := range g.Dismissed {
		lo.Must(io.WriteString(fnv, "dismissed"+dismissed.ComputeHash()))
	}
	return base64.RawURLEncoding.EncodeToString(fnv.Sum(nil))
}

// so far just excludes instructions. Can exclude people and other things
func (g *generatorParams) LocationHash() string {
	fnv := fnv.New64a()
	lo.Must(io.WriteString(fnv, g.Location.ID))
	lo.Must(io.WriteString(fnv, g.Date.Format("2006-01-02")))
	lo.Must(io.WriteString(fnv, staplesSignatureForLocation(g.Location.ID)))
	return base64.RawURLEncoding.EncodeToString(fnv.Sum(nil))
}

func legacyHashToCurrent(hash string, seed string) (string, bool) {
	decoded, err := base64.URLEncoding.DecodeString(hash)
	if err != nil {
		return hash, false
	}
	seedBytes := []byte(seed)
	if !bytes.HasPrefix(decoded, seedBytes) || len(decoded) == len(seedBytes) {
		return hash, false
	}
	return base64.RawURLEncoding.EncodeToString(decoded[len(seedBytes):]), true
}

func ParseGenerationForm(ctx context.Context, r *http.Request, ls locServer) (*generatorParams, error) {
	loc := r.FormValue("location")
	if loc == "" {
		return nil, errors.New("must provide location id")
	}
	if ls == nil {
		return nil, errors.New("location lookup is required")
	}

	l, err := ls.GetLocationByID(ctx, loc)
	if err != nil {
		return nil, err
	}
	now := nowFn()
	dateStr := r.FormValue("date")
	if dateStr != "" {
		parsedDate, err := time.Parse("2006-01-02", dateStr)
		if err != nil {
			return nil, err
		}
		now = parsedDate
	}
	date, err := locations.StoreToDate(ctx, now, l)
	if err != nil {
		return nil, err
	}

	p := DefaultParams(l, date)
	p.Instructions = r.FormValue("instructions")

	return p, nil
}
