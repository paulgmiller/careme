package providerregistry

import (
	"testing"
	"time"

	"careme/internal/farmersmarket"
	"careme/internal/heb"
	"careme/internal/ingredients/cachekey"
	"careme/internal/providers/albertsons"
	"careme/internal/providers/aldi"
	"careme/internal/providers/kroger"
	"careme/internal/providers/mnfoodclub"
	"careme/internal/providers/publix"
	"careme/internal/providers/smithbrothersfarms"
	"careme/internal/providers/walmart"
	"careme/internal/providers/wholefoods"
	"careme/internal/recipes"
)

// this is pretty hacky but if we don't do this we have to pass something to every recipes.IO
// so it can hash params correctly. Open to better methods.
func init() {
	cachekey.RegisterStaplesSignature(StaplesSignature)
	recipes.RegisterRecipeDate(RecipeDate)
}

type recipeDateProvider interface {
	RecipeDate(time.Time) time.Time
}

// RecipeDate applies a provider's recipe cadence to a resolved local store day.
// Providers without a date policy keep daily recipes.
func RecipeDate(locationID string, date time.Time) time.Time {
	for _, provider := range staplesIdentities() {
		if provider.IsID(locationID) {
			if policy, ok := provider.(recipeDateProvider); ok {
				return policy.RecipeDate(date)
			}
			return date
		}
	}
	return date
}

type staplesIdentity interface {
	IsID(string) bool
	Signature() string
}

// StaplesSignature returns the provider version used for a store's recipe and
// ingredient cache keys.
func StaplesSignature(locationID string) string {
	for _, provider := range staplesIdentities() {
		if provider.IsID(locationID) {
			return provider.Signature()
		}
	}
	if testing.Testing() && locationID == "loc-123" {
		return kroger.NewIdentityProvider().Signature()
	}
	panic("unknown staples provider for location " + locationID)
}

func staplesIdentities() []staplesIdentity {
	return []staplesIdentity{
		kroger.NewIdentityProvider(),
		albertsons.NewIdentityProvider(),
		heb.NewIdentityProvider(),
		aldi.NewIdentityProvider(),
		publix.NewIdentityProvider(),
		farmersmarket.NewIdentityProvider(),
		mnfoodclub.NewIdentityProvider(),
		smithbrothersfarms.NewIdentityProvider(),
		wholefoods.NewIdentityProvider(),
		walmart.NewIdentityProvider(),
	}
}
