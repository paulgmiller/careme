package providerregistry

import "careme/internal/locations"

type Factory struct{}

var _ locations.ProviderFactory = Factory{}
