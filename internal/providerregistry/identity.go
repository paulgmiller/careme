package providerregistry

import (
	"hash"
	"testing"
	"time"

	"careme/internal/farmersmarket"
	"careme/internal/ingredients/cachekey"
	"careme/internal/providers/albertsons"
	"careme/internal/providers/aldi"
	"careme/internal/providers/heb"
	"careme/internal/providers/kroger"
	"careme/internal/providers/mnfoodclub"
	"careme/internal/providers/publix"
	"careme/internal/providers/smithbrothersfarms"
	"careme/internal/providers/walmart"
	"careme/internal/providers/wholefoods"
)

// Register cache identities without coupling cache consumers to provider construction.
func init() {
	for _, provider := range staplesIdentities() {
		cachekey.RegisterProvider(provider)
	}
	if testing.Testing() {
		cachekey.RegisterProvider(testKrogerIdentity{})
	}
}

// Preserve the established synthetic Kroger ID used by integration fixtures.
type testKrogerIdentity struct{}

func (testKrogerIdentity) IsID(id string) bool { return id == "loc-123" }
func (testKrogerIdentity) HashForStore(id string, date time.Time) hash.Hash {
	return kroger.NewIdentityProvider().HashForStore(id, date)
}

func staplesIdentities() []cachekey.StoreHasher {
	return []cachekey.StoreHasher{
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
