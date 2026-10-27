package kroger

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"strings"
	"testing"

	"careme/internal/cache"

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
	assert.NotContains(t, page.String(), `hx-get=`)
	assert.NotContains(t, page.String(), `hx-post=`)
	assert.NotContains(t, page.String(), `hx-trigger=`)
	assert.Contains(t, page.String(), `hx-boost="false"`)
	assert.NotContains(t, page.String(), `Connect account`)
	assert.NotContains(t, page.String(), `data-transfer-error`)
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

func TestKrogerOrderTemplateRendersIdempotencyField(t *testing.T) {
	require.NoError(t, templates.Init(&config.Config{}))
	for _, key := range []string{"list", `list"quoted'&<tag>`} {
		t.Run(key, func(t *testing.T) {
			view, err := OrderView(ordertypes.OrderViewInput{ListKey: key})
			require.NoError(t, err)
			var page bytes.Buffer
			require.NoError(t, templates.ShoppingList.ExecuteTemplate(&page, "shopping_order_form", view))
			document, err := html.Parse(strings.NewReader(page.String()))
			require.NoError(t, err)
			values := map[string]string{}
			var inspect func(*html.Node)
			inspect = func(node *html.Node) {
				if node.Type == html.ElementNode && node.Data == "input" {
					attrs := map[string]string{}
					for _, attr := range node.Attr {
						attrs[attr.Key] = attr.Val
					}
					values[attrs["name"]] = attrs["value"]
				}
				for child := node.FirstChild; child != nil; child = child.NextSibling {
					inspect(child)
				}
			}
			inspect(document)
			assert.Equal(t, key, values["idempotency_key"])
			assert.NotContains(t, page.String(), "hx-headers")
		})
	}
}

func TestOrderViewForUserReadsSavedTransfer(t *testing.T) {
	for _, tc := range []struct {
		code, suffix string
		review       bool
	}{
		{"", "", false},
		{"complete", "/submission", true},
		{"uncertain", "/submission", true},
		{"confirmation_failed", "/state", true},
		{"uncertain", "/state", true},
		{"lookup_failed", "/state", false},
		{"start_failed", "/state", false},
		{"no_matches", "/state", false},
		{"pending_login", "/state", false},
	} {
		t.Run(tc.code+tc.suffix, func(t *testing.T) {
			a := newTestOrderHandler(t, &CartClient{})
			transfer := cartTransfer{UserID: "viewer", IdempotencyKey: "list", Request: ordertypes.TransferRequest{Location: locationtypes.Location{ID: "70500874", Chain: "QFC"}}}
			transfer.Result = transfer.result(tc.code, 1, 0)
			if tc.suffix != "" {
				var value any = transfer
				if tc.suffix == "/submission" {
					value = transfer.Result
				}
				require.NoError(t, a.saveJSON(t.Context(), transferKey("viewer", "list")+tc.suffix, value, cache.Unconditional()))
			}
			if tc.suffix == "/submission" {
				require.NoError(t, a.cache.Put(t.Context(), transferKey("viewer", "list")+"/state", "broken", cache.Unconditional()))
			}
			input := ordertypes.OrderViewInput{UserID: "viewer", ListKey: "list", Location: transfer.Request.Location}
			view, err := OrderViewForUser(t.Context(), a.cache, input)
			require.NoError(t, err)
			assert.Equal(t, tc.review, view.ReviewCartURL != "")
			if tc.review {
				assert.Equal(t, "https://www.qfc.com/cart", view.ReviewCartURL)
				assert.Empty(t, view.TransferURL)
				assert.Empty(t, view.TransferRequestJSON)
			}
			input.UserID = "another-viewer"
			other, err := OrderViewForUser(t.Context(), a.cache, input)
			require.NoError(t, err)
			assert.Empty(t, other.ReviewCartURL)
			assert.NotEmpty(t, other.TransferURL)
		})
	}
}

type failingTransferRead struct{ cache.Cache }

func (c failingTransferRead) Get(context.Context, string) (io.ReadCloser, error) {
	return nil, errors.New("read failed")
}

func TestOrderViewForUserReadErrors(t *testing.T) {
	for _, suffix := range []string{"/submission", "/state"} {
		c := cache.NewInMemoryCache()
		require.NoError(t, c.Put(t.Context(), transferKey("viewer", "list")+suffix, "broken", cache.Unconditional()))
		_, err := OrderViewForUser(t.Context(), c, ordertypes.OrderViewInput{UserID: "viewer", ListKey: "list"})
		require.ErrorContains(t, err, "decode Kroger transfer")
	}
	_, err := OrderViewForUser(t.Context(), failingTransferRead{cache.NewInMemoryCache()}, ordertypes.OrderViewInput{UserID: "viewer", ListKey: "list"})
	require.ErrorContains(t, err, "read failed")
}

func TestOrderViewReviewCartReplacesForm(t *testing.T) {
	require.NoError(t, templates.Init(&config.Config{}))
	var page bytes.Buffer
	require.NoError(t, templates.ShoppingList.ExecuteTemplate(&page, "shopping_order_form", ordertypes.OrderView{ReviewCartURL: "https://www.qfc.com/cart"}))
	assert.Contains(t, page.String(), "Review cart (opens a new tab)")
	assert.Contains(t, page.String(), `target="_blank" rel="noopener noreferrer"`)
	assert.NotContains(t, page.String(), "<form")
	assert.NotContains(t, page.String(), "hx-get")
}
