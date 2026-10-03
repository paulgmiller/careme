package providerregistry

import (
	"careme/internal/config"
	"careme/internal/locations"
)

type Factory struct{ config *config.Config }

func NewFactory(cfg *config.Config) Factory { return Factory{config: cfg} }

var _ locations.ProviderFactory = Factory{}
