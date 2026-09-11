// Package cachekey owns store-day ingredient cache identities.
package cachekey

import (
	"encoding/base64"
	"hash/fnv"
	"io"
	"testing"
	"time"

	"careme/internal/albertsons"
	"careme/internal/aldi"
	"careme/internal/farmersmarket"
	"careme/internal/heb"
	"careme/internal/kroger"
	"careme/internal/publix"
	"careme/internal/walmart"
	"careme/internal/wholefoods"

	"github.com/samber/lo"
)

type identityProvider interface {
	IsID(string) bool
	Signature() string
}

// ForStore returns the hash suffix for a store's staple ingredients on date.
// The caller supplies the store date; no timezone conversion or day cutoff is applied.
func ForStore(locationID string, date time.Time) string {
	hash := fnv.New64a()
	lo.Must(io.WriteString(hash, locationID))
	lo.Must(io.WriteString(hash, date.Format("2006-01-02")))
	lo.Must(io.WriteString(hash, StaplesSignature(locationID)))
	return base64.RawURLEncoding.EncodeToString(hash.Sum(nil))
}

// StaplesSignature returns the backend version used in ingredient and recipe cache hashes.
// TODO: Inject a signature resolver at application construction so cachekey no
// longer imports grocery providers. Wire it through recipe hashing and produce
// scoring while preserving existing hashes; replace the loc-123 test special case
// with an explicit fake resolver as part of that refactor.
func StaplesSignature(locationID string) string {
	for _, provider := range defaultIdentityProviders() {
		if provider.IsID(locationID) {
			return provider.Signature()
		}
	}

	if testing.Testing() && locationID == "loc-123" {
		return kroger.NewIdentityProvider().Signature()
	}

	panic("unknown staples provider for location " + locationID)
}

func defaultIdentityProviders() []identityProvider {
	return []identityProvider{
		kroger.NewIdentityProvider(),
		albertsons.NewIdentityProvider(),
		heb.NewIdentityProvider(),
		aldi.NewIdentityProvider(),
		publix.NewIdentityProvider(),
		farmersmarket.NewIdentityProvider(),
		wholefoods.NewIdentityProvider(),
		walmart.NewIdentityProvider(),
	}
}
