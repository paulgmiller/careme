package farmersmarket

import (
	"hash"
	"strings"
	"time"

	"careme/internal/ingredients/cachekey"
)

type identityProvider struct{}

func (p identityProvider) HashForStore(locationID string, date time.Time) hash.Hash {
	date = cachekey.StoreDay(date)
	return cachekey.NewHash(locationID, date, signature)
}

func NewIdentityProvider() identityProvider {
	return identityProvider{}
}

func (s identityProvider) IsID(locationID string) bool {
	return isID(locationID)
}

func isID(locationID string) bool {
	return strings.HasPrefix(locationID, LocationIDPrefix) && strings.TrimPrefix(locationID, LocationIDPrefix) != ""
}
