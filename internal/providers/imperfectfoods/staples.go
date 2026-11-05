package imperfectfoods

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"fmt"
	"hash"
	"time"

	"careme/internal/ai"
	"careme/internal/ingredients/cachekey"
)

//go:embed snapshot.json
var snapshotJSON []byte

const locationID = "imperfectfoods_demo"

type identityProvider struct{}

func NewIdentityProvider() identityProvider { return identityProvider{} }

func (identityProvider) IsID(id string) bool { return id == locationID }

func (identityProvider) HashForStore(id string, date time.Time) hash.Hash {
	return snapshotHash(id, date, snapshotJSON)
}

func snapshotHash(id string, date time.Time, raw []byte) hash.Hash {
	signature := fmt.Sprintf("imperfectfoods-snapshot-v1/%x", sha256.Sum256(raw))
	return cachekey.NewHash(id, cachekey.StoreDay(date), signature)
}

type staplesProvider struct{ identityProvider }

func NewStaplesProvider() staplesProvider { return staplesProvider{} }

func (p staplesProvider) FetchStaples(ctx context.Context, id string) ([]ai.InputIngredient, error) {
	data, err := p.load(ctx, id)
	if err != nil {
		return nil, err
	}
	return data.staples, nil
}

// FetchWines returns the fixed wine catalog; recipe wine selection handles styles.
func (p staplesProvider) FetchWines(ctx context.Context, id string, _ []string) ([]ai.InputIngredient, error) {
	data, err := p.load(ctx, id)
	if err != nil {
		return nil, err
	}
	return data.wines, nil
}

func (p staplesProvider) load(ctx context.Context, id string) (catalog, error) {
	if err := ctx.Err(); err != nil {
		return catalog{}, err
	}
	if !p.IsID(id) {
		return catalog{}, fmt.Errorf("invalid Imperfect Foods location ID %q", id)
	}
	data, err := parseSnapshot(snapshotJSON)
	if err != nil {
		return catalog{}, fmt.Errorf("load Imperfect Foods demo catalog: %w", err)
	}
	return data, nil
}
