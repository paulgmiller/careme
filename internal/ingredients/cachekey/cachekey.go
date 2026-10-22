// Package cachekey owns provider-specific ingredient cache identities.
package cachekey

import (
	"encoding/base64"
	"hash"
	"hash/fnv"
	"io"
	"testing"
	"time"

	"github.com/samber/lo"
)

// StoreHasher owns a provider's cache cadence and staple version.
type StoreHasher interface {
	IsID(string) bool
	HashForStore(string, time.Time) hash.Hash
}

var providers []StoreHasher

// RegisterProvider installs a provider during application initialization.
func RegisterProvider(provider StoreHasher) {
	providers = append(providers, provider)
}

func providerFor(locationID string) StoreHasher {
	for _, provider := range providers {
		if provider.IsID(locationID) {
			return provider
		}
	}
	if testing.Testing() && len(providers) == 0 {
		return testProvider{}
	}
	panic("unknown staples provider for location " + locationID)
}

// ForStore returns the cache suffix for a store-local timestamp.
func ForStore(locationID string, date time.Time) string {
	return base64.RawURLEncoding.EncodeToString(HashForStore(locationID, date).Sum(nil))
}

// HashForStore returns a fresh hash, ready for additional recipe-specific input.
func HashForStore(locationID string, date time.Time) hash.Hash {
	return providerFor(locationID).HashForStore(locationID, date)
}

// StoreDay resolves a store-local timestamp using the 9 AM shopping-day boundary.
// A date with no time component is already a resolved store day.
func StoreDay(date time.Time) time.Time {
	hour, minute, second := date.Clock()
	// Legacy params stored resolved store days at midnight. Preserve those dates
	// so existing daily cache hashes stay stable without a separate legacy path.
	if hour == 0 && minute == 0 && second == 0 && date.Nanosecond() == 0 {
		return date
	}
	if hour < 9 {
		date = date.AddDate(0, 0, -1)
	}
	return time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, date.Location())
}

// NewHash hashes an already resolved cache date without applying date policy.
func NewHash(locationID string, date time.Time, signature string) hash.Hash {
	h := fnv.New64a()
	lo.Must(io.WriteString(h, locationID))
	lo.Must(io.WriteString(h, date.Format(time.DateOnly)))
	lo.Must(io.WriteString(h, signature))
	return h
}

type testProvider struct{}

func (testProvider) IsID(string) bool  { return true }
func (testProvider) Signature() string { return "test" }
func (p testProvider) HashForStore(id string, date time.Time) hash.Hash {
	return NewHash(id, StoreDay(date), p.Signature())
}
