package providerregistry

import (
	"careme/internal/config"
)

type Factory struct{ config *config.Config }

func NewFactory(cfg *config.Config) Factory { return Factory{config: cfg} }
