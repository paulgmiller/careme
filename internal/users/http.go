package users

import (
	"context"
	"errors"
	"net/http"
	"time"
)

type contextKey string

const userContextKey contextKey = "careme_user"

// ContextWithUser stores the user in context for request-scoped access.
func ContextWithUser(ctx context.Context, user *User) context.Context {
	return context.WithValue(ctx, userContextKey, user)
}

// UserFromContext returns the user stored in context, if any.
func UserFromContext(ctx context.Context) (*User, bool) {
	user, ok := ctx.Value(userContextKey).(*User)
	return user, ok && user != nil
}

// SetCookie stores the user identifier in the browser for the given duration.
func SetCookie(w http.ResponseWriter, userID string, duration time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    userID,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(duration),
		MaxAge:   int(duration / time.Second),
	})
}

// ClearCookie removes the stored user identifier from the browser.
func ClearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	})
}

// FromRequest extracts the current user from the incoming request cookie.
func FromRequest(r *http.Request, store *Storage) (*User, error) {
	if user, ok := UserFromContext(r.Context()); ok {
		return user, nil
	}
	cookie, err := r.Cookie(CookieName)
	if err != nil {
		if errors.Is(err, http.ErrNoCookie) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if cookie.Value == "" {
		return nil, ErrNotFound
	}
	user, err := store.GetByID(cookie.Value)
	if err != nil {
		return nil, err
	}
	return user, nil
}
