package farmersmarket

import "strings"

type identityProvider struct{}

func NewIdentityProvider() identityProvider {
	return identityProvider{}
}

func (s identityProvider) IsID(locationID string) bool {
	return isID(locationID)
}

func (s identityProvider) Signature() string {
	return signature
}

func isID(locationID string) bool {
	return strings.HasPrefix(locationID, LocationIDPrefix) && strings.TrimPrefix(locationID, LocationIDPrefix) != ""
}
