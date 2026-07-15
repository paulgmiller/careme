package guest

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	ShoppingListCookieName = "careme_guest_shopping_lists"
	ShoppingListLimit      = 2
	shoppingListCookieAge  = 90 * 24 * time.Hour
)

func ShoppingListCount(r *http.Request) (int, bool) {
	cookie, err := r.Cookie(ShoppingListCookieName)
	if err != nil {
		return 0, false
	}
	count, err := strconv.Atoi(strings.TrimSpace(cookie.Value))
	if err != nil || count < 0 {
		return 0, false
	}
	return count, true
}

func SetShoppingListCount(w http.ResponseWriter, r *http.Request, count int) {
	http.SetCookie(w, &http.Cookie{
		Name:     ShoppingListCookieName,
		Value:    strconv.Itoa(count),
		Path:     "/",
		MaxAge:   int(shoppingListCookieAge.Seconds()),
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
	})
}

func EnsureShoppingListCount(w http.ResponseWriter, r *http.Request) {
	if _, ok := ShoppingListCount(r); ok {
		return
	}
	SetShoppingListCount(w, r, 0)
}
