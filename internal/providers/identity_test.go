package providers

import (
	"testing"

	"careme/internal/providers/mnfoodclub"
	"careme/internal/providers/smithbrothersfarms"
	"github.com/stretchr/testify/assert"
)

func TestCatalogProviderID(t *testing.T) {
	for _, tc := range []struct{ id, provider string }{
		{"70500874", "kroger"},
		{smithbrothersfarms.LocationIDPrefix + "delivery", "smithbrothersfarms"},
		{mnfoodclub.LocationIDPrefix + "delivery", "mnfoodclub"},
		{"walmart_123", "walmart"},
		{"unknown-store", ""},
	} {
		t.Run(tc.id, func(t *testing.T) { assert.Equal(t, tc.provider, CatalogProviderID(tc.id)) })
	}
}
