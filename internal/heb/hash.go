package heb

import (
	"hash"
	"time"

	"careme/internal/ingredients/cachekey"
)

func (p identityProvider) HashForStore(locationID string, date time.Time) hash.Hash {
	date = cachekey.StoreDay(date)
	return cachekey.NewHash(locationID, date, p.Signature())
}
