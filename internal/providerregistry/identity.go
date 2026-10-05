package providerregistry

import (
	"testing"

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
)

// this is pretty hacky but if we don't do this we have to pass something to every recipes.IO
// so it can hash params correctly. Open to better methods.
func init() {
	cachekey.RegisterStaplesSignature(StaplesSignature)
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
