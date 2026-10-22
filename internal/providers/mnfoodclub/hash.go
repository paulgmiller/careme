package mnfoodclub

import (
	"hash"
	"time"

	"careme/internal/ingredients/cachekey"
)

func (p identityProvider) HashForStore(locationID string, date time.Time) hash.Hash {
	date = cachekey.StoreDay(date)
	date = date.AddDate(0, 0, -((int(date.Weekday()) + 6) % 7))
	return cachekey.NewHash(locationID, date, p.Signature())
}
