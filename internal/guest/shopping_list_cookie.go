package guest

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	ShoppingListCookieName = "careme_guest_shopping_lists"
	shoppingListLimit      = 2
	shoppingListCookieAge  = 90 * 24 * time.Hour
)

func shoppingListCount(r *http.Request) (int, bool) {
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

func setShoppingListCount(w http.ResponseWriter, r *http.Request, count int) {
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

func UseShoppingList(w http.ResponseWriter, r *http.Request) bool {
	count, ok := shoppingListCount(r)
	if !ok || count >= shoppingListLimit {
		return false
	}
	setShoppingListCount(w, r, count+1)
	return true
}

func EnsureShoppingListCount(w http.ResponseWriter, r *http.Request) {
	if _, ok := shoppingListCount(r); ok {
		return
	}
	setShoppingListCount(w, r, 0)
}
