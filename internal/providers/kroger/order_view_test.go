package kroger

import (
	"bytes"
	"encoding/json"
	"math"
	"strings"
	"testing"

	ordertypes "careme/internal/orders/types"

	"careme/internal/ai"
	"careme/internal/config"
	locationtypes "careme/internal/locations/types"
	"careme/internal/templates"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/html"
)

func TestKrogerOrderTransferTemplate(t *testing.T) {
	require.NoError(t, templates.Init(&config.Config{}))
	order, err := OrderView(ordertypes.OrderViewInput{Location: locationtypes.Location{ID: "70500874", Chain: "QFC"}, ListKey: "list"})
	require.NoError(t, err)
	var page bytes.Buffer
	require.NoError(t, templates.ShoppingList.ExecuteTemplate(&page, "shopping_finalize_controls", map[string]any{"Order": order, "HasSavedRecipes": true}))
	assert.Contains(t, page.String(), `action="/kroger/cart-transfers"`)
	assert.Contains(t, page.String(), `hx-get="/kroger/cart-transfers/`+"list"+`"`)
	assert.Contains(t, page.String(), `hx-post="/kroger/cart-transfers"`)
	assert.Contains(t, page.String(), `hx-trigger="load"`)
	assert.Contains(t, page.String(), `hx-disabled-elt="find button[type=submit]"`)
	assert.Contains(t, page.String(), `data-transfer-error hidden role="alert"`)
	assert.NotContains(t, page.String(), "Build Shopping List")
}

func TestKrogerOrderViewRejectsInvalidItems(t *testing.T) {
	invalid := math.NaN()
	_, err := OrderView(ordertypes.OrderViewInput{Items: []ai.ShoppingItem{{Name: "Invalid", Buy: &invalid}}})
	require.ErrorContains(t, err, "encode Kroger transfer request")
}

func TestKrogerOrderViewForm(t *testing.T) {
	require.NoError(t, templates.Init(&config.Config{}))
	for _, chain := range []string{"QFC", ""} {
		view, err := OrderView(ordertypes.OrderViewInput{Location: locationtypes.Location{ID: "70500874", Chain: chain}, ListKey: "list", ReturnURL: "/recipes?h=list"})
		require.NoError(t, err)
		assert.Equal(t, "/kroger/cart-transfers", view.TransferURL)
		brand := chain
		if brand == "" {
			brand = "Kroger"
		}
		assert.Equal(t, "Add to "+brand+" cart", view.ActionLabel)
		var request ordertypes.TransferRequest
		require.NoError(t, json.Unmarshal([]byte(view.TransferRequestJSON), &request))
		assert.Equal(t, "70500874", request.Location.ID)
		assert.Equal(t, chain, request.Location.Chain)
		assert.Equal(t, "list", view.IdempotencyKey)
		assert.Equal(t, "/recipes?h=list", request.ReturnURL)
	}
}

func TestKrogerOrderViewRejectsInvalidLocation(t *testing.T) {
	invalid := math.NaN()
	_, err := OrderView(ordertypes.OrderViewInput{Location: locationtypes.Location{Lat: &invalid}})
	require.ErrorContains(t, err, "encode Kroger transfer request")
}

func TestKrogerTransferUsesChainCart(t *testing.T) {
	for banner, domain := range chainDomains {
		response := (cartTransfer{IdempotencyKey: "list", Request: ordertypes.TransferRequest{Location: locationtypes.Location{Chain: banner}}}).result("complete", 0, 0)
		assert.Equal(t, "https://www."+domain+"/cart", response.CartURL)
		assert.Equal(t, "applied", response.Status)
	}
}

func TestKrogerOrderTemplateRendersIdempotencyHeader(t *testing.T) {
	require.NoError(t, templates.Init(&config.Config{}))
	for _, key := range []string{"list", `list"quoted'&<tag>`} {
		t.Run(key, func(t *testing.T) {
			view, err := OrderView(ordertypes.OrderViewInput{ListKey: key})
			require.NoError(t, err)
			var page bytes.Buffer
			require.NoError(t, templates.ShoppingList.ExecuteTemplate(&page, "shopping_order_form", view))
			document, err := html.Parse(strings.NewReader(page.String()))
			require.NoError(t, err)
			var headers map[string]string
			var inspect func(*html.Node)
			inspect = func(node *html.Node) {
				if node.Type == html.ElementNode && node.Data == "form" {
					for _, attr := range node.Attr {
						if attr.Key == "hx-headers" {
							require.NoError(t, json.Unmarshal([]byte(attr.Val), &headers))
						}
					}
				}
				for child := node.FirstChild; child != nil; child = child.NextSibling {
					inspect(child)
				}
			}
			inspect(document)
			assert.Equal(t, map[string]string{"Idempotency-Key": key}, headers)
			assert.NotContains(t, page.String(), `name="idempotency_key"`)
		})
	}
}
