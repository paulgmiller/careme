package guest

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUseShoppingList(t *testing.T) {
	tests := []struct {
		name            string
		cookieValue     string
		wantAllowed     bool
		wantCookieValue string
	}{
		{name: "missing cookie"},
		{name: "invalid cookie", cookieValue: "wat"},
		{name: "limit reached", cookieValue: "2"},
		{name: "first use", cookieValue: "0", wantAllowed: true, wantCookieValue: "1"},
		{name: "last use", cookieValue: "1", wantAllowed: true, wantCookieValue: "2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/recipes", nil)
			if tt.cookieValue != "" {
				req.AddCookie(&http.Cookie{Name: ShoppingListCookieName, Value: tt.cookieValue})
			}
			rr := httptest.NewRecorder()

			assert.Equal(t, tt.wantAllowed, UseShoppingList(rr, req))

			cookies := rr.Result().Cookies()
			if tt.wantCookieValue == "" {
				assert.Empty(t, cookies)
				return
			}
			require.Len(t, cookies, 1)
			assert.Equal(t, ShoppingListCookieName, cookies[0].Name)
			assert.Equal(t, tt.wantCookieValue, cookies[0].Value)
		})
	}
}
