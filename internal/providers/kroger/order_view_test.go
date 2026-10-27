package kroger

import (
	"bytes"
	"encoding/json"
	"math"
	"testing"

	ordertypes "careme/internal/orders/types"

	"careme/internal/ai"
	"careme/internal/config"
	locationtypes "careme/internal/locations/types"
	"careme/internal/templates"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKrogerOrderTransferTemplate(t *testing.T) {
	require.NoError(t, templates.Init(&config.Config{}))
	order, err := OrderView(ordertypes.OrderViewInput{Location: locationtypes.Location{ID: "70500874", Chain: "QFC"}, ListKey: "list"})
	require.NoError(t, err)
	var page bytes.Buffer
	require.NoError(t, templates.ShoppingList.ExecuteTemplate(&page, "shopping_finalize_controls", map[string]any{"Order": order, "HasSavedRecipes": true}))
	assert.Contains(t, page.String(), `action="/kroger/cart-transfers"`)
	assert.Contains(t, page.String(), `hx-get="/kroger/cart-transfers/`+ordertypes.TransferID("list")+`"`)
	assert.Contains(t, page.String(), `hx-post="/kroger/cart-transfers"`)
	assert.Contains(t, page.String(), `hx-trigger="load"`)
	assert.Contains(t, page.String(), `hx-disabled-elt="find button[type=submit]"`)
	assert.Contains(t, page.String(), `data-transfer-error hidden role="alert"`)
	assert.NotContains(t, page.String(), "Build Shopping List")
}

func TestKrogerOrderViewRejectsInvalidItems(t *testing.T) {
	invalid := math.NaN()
	_, err := OrderView(ordertypes.OrderViewInput{Items: []ai.ShoppingItem{{Name: "Invalid", Buy: &invalid}}})
	require.ErrorContains(t, err, "encode shopping items for Kroger")
}

func TestKrogerOrderViewForm(t *testing.T) {
	require.NoError(t, templates.Init(&config.Config{}))
	for _, chain := range []string{"QFC", ""} {
		view, err := OrderView(ordertypes.OrderViewInput{Location: locationtypes.Location{ID: "70500874", Chain: chain}, ListKey: "list", ReturnURL: "/recipes?h=list"})
		require.NoError(t, err)
		assert.Equal(t, "/kroger/cart-transfers", view.ActionURL)
		brand := chain
		if brand == "" {
			brand = "Kroger"
		}
		assert.Equal(t, "Add to "+brand+" cart", view.ActionLabel)
		var location locationtypes.Location
		require.NoError(t, json.Unmarshal([]byte(view.Fields.Location), &location))
		assert.Equal(t, "70500874", location.ID)
		assert.Equal(t, chain, location.Chain)
		assert.JSONEq(t, `{"Idempotency-Key":"list"}`, view.Headers)
		assert.Equal(t, "/recipes?h=list", view.Fields.ReturnURL)
	}
}

func TestKrogerOrderViewRejectsInvalidLocation(t *testing.T) {
	invalid := math.NaN()
	_, err := OrderView(ordertypes.OrderViewInput{Location: locationtypes.Location{Lat: &invalid}})
	require.ErrorContains(t, err, "encode ordering location")
}

func TestKrogerTransferUsesChainCart(t *testing.T) {
	for banner, domain := range chainDomains {
		response := (cartTransfer{Request: orderRequest{Chain: banner, IdempotencyKey: "list"}, Result: orderResult{Status: "complete"}}).response()
		assert.Equal(t, "https://www."+domain+"/cart", response.CartURL)
		assert.Equal(t, "applied", response.Status)
	}
}
