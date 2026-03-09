package recipes

import (
	"bytes"
	"careme/internal/ai"
	"careme/internal/locations"
	"context"
	"encoding/base64"
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

const (
	legacyRecipeHashSeed      = "recipe"
	legacyIngredientsHashSeed = "ingredients"
	storeDayStartHour         = 9
)

var nowFn = time.Now

type generatorParams struct {
	Location *locations.Location `json:"location,omitempty"`
	Date     time.Time           `json:"date,omitempty"`
	// People       int
	//per round instuctions
	Instructions string   `json:"instructions,omitempty"`
	Directive    string   `json:"directive,omitempty"` // this is the new one that will be used. Can remove GenerationPrompt after a while.
	LastRecipes  []string `json:"last_recipes,omitempty"`
	// UserID         string      `json:"user_id,omitempty"`
	ConversationID string `json:"conversation_id,omitempty"` // Can remove if we pass it in separately to generate recipes?
	//TODO Both should just be title and hash insread of full ai.Recipe
	Saved     []ai.Recipe `json:"saved_recipes,omitempty"`
	Dismissed []ai.Recipe `json:"dismissed_recipes,omitempty"`
}

func DefaultParams(l *locations.Location, date time.Time) *generatorParams {
	// normalize to midnight (shave hours, minutes, seconds, nanoseconds)
	date = time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, date.Location())
	return &generatorParams{
		Date:     date, // shave time
		Location: l,
		// People:   2,
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

func (s *server) ParseQueryArgs(ctx context.Context, r *http.Request) (*generatorParams, error) {
	loc := r.URL.Query().Get("location")
	if loc == "" {
		return nil, errors.New("must provide location id")
	}

	l, err := s.locServer.GetLocationByID(ctx, loc)
	if err != nil {
		return nil, err
	}

	storeLoc, err := resolveStoreTimeLocation(ctx, l)
	if err != nil {
		return nil, err
	}
	dateStr := r.URL.Query().Get("date")
	date := defaultRecipeDate(nowFn(), storeLoc)
	if dateStr != "" {
		parsedDate, err := time.ParseInLocation("2006-01-02", dateStr, storeLoc)
		if err != nil {
			return nil, err
		}
		date = parsedDate
	}

	p := DefaultParams(l, date)
	p.Instructions = r.URL.Query().Get("instructions")
	// should this be in hash?
	p.ConversationID = strings.TrimSpace(r.URL.Query().Get("conversation_id"))

	return p, nil
}

func resolveStoreTimeLocation(ctx context.Context, l *locations.Location) (*time.Location, error) {
	if l == nil {
		return nil, fmt.Errorf("nil location")
	}
	tzName, ok := timezoneNameForZip(l.ZipCode)
	if !ok {
		return nil, fmt.Errorf("unable to infer timezone from zipcode %s", l.ZipCode)
	}
	storeLoc, err := time.LoadLocation(tzName)
	if err != nil {
		slog.ErrorContext(ctx, "invalid inferred timezone; falling back to UTC", "location_id", l.ID, "zipcode", l.ZipCode, "timezone", tzName, "error", err)
		return nil, err
	}
	return storeLoc, nil
}

func timezoneNameForZip(zip string) (string, bool) {
	trimmed := strings.TrimSpace(zip)
	if trimmed == "" {
		return "", false
	}
	switch first := trimmed[0]; {
	case first >= '0' && first <= '3':
		return "America/New_York", true
	case first >= '4' && first <= '7':
		return "America/Chicago", true
	case first == '8':
		return "America/Denver", true
	case first == '9':
		return "America/Los_Angeles", true
	default:
		return "", false
	}
}

func StoreToDate(ctx context.Context, now time.Time, l *locations.Location) (time.Time, error) {
	tz, err := resolveStoreTimeLocation(ctx, l)
	if err != nil {
		return now, err
	}
	return defaultRecipeDate(now, tz), nil
}

func defaultRecipeDate(now time.Time, storeLoc *time.Location) time.Time {
	localNow := now.In(storeLoc)
	if localNow.Hour() < storeDayStartHour {
		localNow = localNow.AddDate(0, 0, -1)
	}
	return time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, storeLoc)
}
