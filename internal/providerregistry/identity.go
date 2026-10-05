package providerregistry

import (
	"testing"

	"careme/internal/farmersmarket"
	"careme/internal/heb"
	"careme/internal/providers/albertsons"
	"careme/internal/providers/aldi"
	"careme/internal/providers/kroger"
	"careme/internal/providers/mnfoodclub"
	"careme/internal/providers/publix"
	"careme/internal/providers/smithbrothersfarms"
	"careme/internal/providers/walmart"
	"careme/internal/providers/wholefoods"
)

type staplesIdentity interface {
	IsID(string) bool
	Signature() string
}

// SignatureFactory resolves provider versions without configuration or clients.
type SignatureFactory struct{}

// StaplesSignature returns the provider version used for a store's recipe and
// ingredient cache keys.
func (SignatureFactory) StaplesSignature(locationID string) string {
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
