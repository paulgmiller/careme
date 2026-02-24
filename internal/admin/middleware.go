package admin

import (
	"careme/internal/auth"
	"careme/internal/config"
	"errors"
	"log/slog"
	"net/http"
	"strings"
)

type Middleware struct {
	auth   auth.AuthClient
	admins map[string]struct{}
}

func NewMiddleware(cfg *config.Config, authClient auth.AuthClient) *Middleware {
	admins := make(map[string]struct{}, len(cfg.Admin.Emails))
	for _, email := range cfg.Admin.Emails {
		normalized := normalizeEmail(email)
		if normalized == "" {
			continue
		}
		admins[normalized] = struct{}{}
	}

	return &Middleware{
		auth:   authClient,
		admins: admins,
	}
}

func (m *Middleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID, err := m.auth.GetUserIDFromRequest(r)
		if err != nil {
			if !errors.Is(err, auth.ErrNoSession) {
				slog.WarnContext(r.Context(), "admin auth failed", "error", err)
			}
			http.NotFound(w, r)
			return
		}

		email, err := m.auth.GetUserEmail(r.Context(), userID)
		if err != nil {
			slog.WarnContext(r.Context(), "admin email lookup failed", "user_id", userID, "error", err)
			http.NotFound(w, r)
			return
		}

		if !m.IsAdmin(email) {
			http.NotFound(w, r)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (m *Middleware) IsAdmin(email string) bool {
	if len(m.admins) == 0 {
		return true
	}
	_, ok := m.admins[normalizeEmail(email)]
	return ok
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
