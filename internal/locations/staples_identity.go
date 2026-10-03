package locations

import "sync"

var (
	staplesSignatureMu sync.RWMutex
	staplesSignature   func(string) string
)

// RegisterStaplesSignature installs the provider-specific signature resolver.
// It must be called during application initialization, before serving requests.
func RegisterStaplesSignature(resolve func(string) string) {
	if resolve == nil {
		panic("nil staples signature resolver")
	}
	staplesSignatureMu.Lock()
	defer staplesSignatureMu.Unlock()
	if staplesSignature != nil {
		panic("staples signature resolver already registered")
	}
	staplesSignature = resolve
}

func StaplesSignature(locationID string) string {
	staplesSignatureMu.RLock()
	resolve := staplesSignature
	staplesSignatureMu.RUnlock()
	if resolve == nil {
		panic("staples signature resolver not registered")
	}
	return resolve(locationID)
}
