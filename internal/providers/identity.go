package providers

import (
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

type catalogProvider interface {
	IsID(string) bool
}

// CatalogProviderID identifies the namespace of product references from a location.
// Unknown locations carry no inferred product namespace.
func CatalogProviderID(locationID string) string {
	for _, entry := range catalogIdentities() {
		if entry.identity.IsID(locationID) {
			return entry.id
		}
	}
	return ""
}

type catalogIdentity struct {
	id       string
	identity catalogProvider
}

func catalogIdentities() []catalogIdentity {
	return []catalogIdentity{
		{"kroger", kroger.NewIdentityProvider()},
		{"albertsons", albertsons.NewIdentityProvider()},
		{"heb", heb.NewIdentityProvider()},
		{"aldi", aldi.NewIdentityProvider()},
		{"publix", publix.NewIdentityProvider()},
		{"farmersmarket", farmersmarket.NewIdentityProvider()},
		{"mnfoodclub", mnfoodclub.NewIdentityProvider()},
		{"smithbrothersfarms", smithbrothersfarms.NewIdentityProvider()},
		{"wholefoods", wholefoods.NewIdentityProvider()},
		{"walmart", walmart.NewIdentityProvider()},
	}
}
