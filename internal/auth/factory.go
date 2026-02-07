package auth

import "careme/internal/config"

// NewFromConfig creates an AuthClient based on config settings.
func NewFromConfig(cfg *config.Config) (AuthClient, error) {
	if cfg.Mocks.Enable {
		return Mock(cfg), nil
	}

	return NewClient(cfg)
}
