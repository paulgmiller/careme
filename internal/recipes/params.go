package recipes

import (
	"bytes"
	"careme/internal/ai"
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

const (
	legacyRecipeHashSeed      = "recipe"
	legacyIngredientsHashSeed = "ingredients"
	storeDayStartHour         = 9
)

var nowFn = time.Now

type generatorParams struct {
	Location *locations.Location `json:"location,omitempty"`
	Date     time.Time           `json:"date,omitempty"`
	Staples  []filter            `json:"staples,omitempty"`
	// People       int
	Instructions string   `json:"instructions,omitempty"`
	LastRecipes  []string `json:"last_recipes,omitempty"`
	// UserID         string      `json:"user_id,omitempty"`
	ConversationID string      `json:"conversation_id,omitempty"` // Can remove if we pass it in separately to generate recipes?
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
	return base64.RawURLEncoding.EncodeToString(fnv.Sum(nil))
}

// so far just excludes instructions. Can exclude people and other things
func (g *generatorParams) LocationHash() string {
	fnv := fnv.New64a()
	lo.Must(io.WriteString(fnv, g.Location.ID))
	lo.Must(io.WriteString(fnv, g.Date.Format("2006-01-02")))
	bytes := lo.Must(json.Marshal(g.Staples)) // excited fro this to break in some weird way
	lo.Must(fnv.Write(bytes))
	return base64.RawURLEncoding.EncodeToString(fnv.Sum(nil))
}

func normalizeLegacyRecipeHash(hash string) (string, bool) {
	return legacyHashToCurrent(hash, legacyRecipeHashSeed)
}

func legacyRecipeHash(hash string) (string, bool) {
	return currentHashToLegacy(hash, legacyRecipeHashSeed)
}

func legacyLocationHash(hash string) (string, bool) {
	return currentHashToLegacy(hash, legacyIngredientsHashSeed)
}

func legacyHashToCurrent(hash string, seed string) (string, bool) {
	decoded, err := base64.URLEncoding.DecodeString(hash)
	if err != nil {
		return "", false
	}
	seedBytes := []byte(seed)
	if !bytes.HasPrefix(decoded, seedBytes) || len(decoded) == len(seedBytes) {
		return "", false
	}
	return base64.RawURLEncoding.EncodeToString(decoded[len(seedBytes):]), true
}

func currentHashToLegacy(hash string, seed string) (string, bool) {
	decoded, err := base64.RawURLEncoding.DecodeString(hash)
	if err != nil || len(decoded) == 0 {
		return "", false
	}
	seedBytes := []byte(seed)
	if bytes.HasPrefix(decoded, seedBytes) {
		return hash, false
	}
	legacyDecoded := make([]byte, 0, len(seedBytes)+len(decoded))
	legacyDecoded = append(legacyDecoded, seedBytes...)
	legacyDecoded = append(legacyDecoded, decoded...)
	return base64.URLEncoding.EncodeToString(legacyDecoded), true
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
	// TODO is it overkill to pull full recip in param instead of just hash?
	for _, hash := range savedHashes {
		recipe, err := s.SingleFromCache(ctx, hash)
		if err != nil {
			slog.ErrorContext(ctx, "failed to load saved recipe by hash", "hash", hash, "error", err)
			continue
		}
		recipe.Saved = true
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
	return append(Produce(), []filter{
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
	}...)
}

// This is dramatically overfit to my qfc
func Produce() []filter {
	return []filter{
		{
			//Produce score  0.532710: 57/107 with 294 ingredients
			Term:   "produce",
			Brands: []string{"*"}, // ther's alot of fresh * and kroger here. cut this down after 500 sadness
		},
		{
			Term:   "mushrooms produce",
			Brands: []string{"*"}, // ther's alot of fresh * and kroger here. cut this down after 500 sadness
		},
		/*
					miller@millerbase [03:13:24 PM] [~/careme/cmd/ingredients] [codex/add-acceptance-test-for-produce-availability *]
			-> % go run . -l 70500874 -i "peppers produce"
			2026/02/21 15:13:30 Using Azure Blob Storage for cache
			Simple Truth Organic - Simple Truth Organic® Roma Tomatoes:([Produce Natural & Organic Produce]))
			Simple Truth Organic - Simple Truth Organic® Tomato Medley Snacking Tomatoes:([Natural & Organic]))
			Fresh Tomatoes - Fresh Organic On the Vine Tomatoes (4-5 Tomatoes per Bunch):([Natural & Organic Produce Produce]))
			Simple Truth Organic - Simple Truth Organic® Fresh Grape Snacking Tomatoes:([Natural & Organic Produce Produce]))
			pmiller@millerbase [03:13:31 PM] [~/careme/cmd/ingredients] [codex/add-acceptance-test-for-produce-availability *]
			-> % go run . -l 70500874 -i "habenero produce"
			2026/02/21 15:13:45 Using Azure Blob Storage for cache
			 - Fresh Jalapeno Peppers:([Produce International]))
			Maple Leaf - Maple Leaf Habenero Jack:([Deli]))
			 - Fresh Orange Bell Pepper:([International Produce]))
			Fresh Onions - Jumbo White Onions:([Produce]))
			Fresh Tomatoes - Fresh Roma Tomato:([Produce]))
			 - Pasilla Peppers:([International Produce]))
			 - Fresh Tomatillo:([Produce International Produce]))
			Fresh Tomatoes - Fresh Green Tomato:([Produce]))
			 - Fresh Habanero Peppers:([International Produce]))
			 - Fresh Anaheim Peppers:([International Produce]))
			 - Fresh Hatch Peppers:([International Produce]))
			 - Fresh Yellow Bell Pepper:([International Produce]))
			 - Fresh Green Serrano Peppers:([Produce International]))
			 - Fresh Poblano Peppers:([Produce International]))
		*/
		{
			Term:   "habenero produce",
			Brands: []string{"*"}, // ther's alot of fresh * and kroger here. cut this down after 500 sadness
		},
		{
			Term: "bell peppers",
		},
		{
			Term:   "cucumber produce",
			Brands: []string{"*"}, // ther's alot of fresh * and kroger here. cut this down after 500 sadness
		},
	}
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
