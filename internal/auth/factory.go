package auth

import "careme/internal/config"

// NewFromConfig creates an AuthClient based on config settings.
func NewFromConfig(cfg *config.Config, userExists UserExistsFunc) (AuthClient, error) {
	if cfg.Mocks.Enable {
		return Mock(cfg, userExists), nil
	}

	return NewClient(cfg, userExists)
}
