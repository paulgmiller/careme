// Package cachekey owns store-day ingredient cache identities.
package cachekey

import (
	"encoding/base64"
	"hash"
	"hash/fnv"
	"io"
	"time"

	"github.com/samber/lo"
)

// ForStore returns the hash suffix for a store's staple ingredients on date.
// The caller supplies the store date; no timezone conversion or day cutoff is applied.
func ForStore(locationID string, date time.Time, signature string) string {
	hash := HashForStore(locationID, date, signature)
	return base64.RawURLEncoding.EncodeToString(hash.Sum(nil))
}

func HashForStore(locationID string, date time.Time, signature string) hash.Hash {
	hash := fnv.New64a()
	lo.Must(io.WriteString(hash, locationID))
	lo.Must(io.WriteString(hash, date.Format("2006-01-02")))
	lo.Must(io.WriteString(hash, signature))
	return hash
}
