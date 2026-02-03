package auth

import (
	"careme/internal/config"
	"careme/internal/users"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/clerk/clerk-sdk-go/v2"
	"github.com/clerk/clerk-sdk-go/v2/jwks"
	"github.com/clerk/clerk-sdk-go/v2/jwt"
	"github.com/clerk/clerk-sdk-go/v2/user"
)

const (
	clerkSessionCookie   = "__session"
	localSessionDuration = 365 * 24 * time.Hour
)

type tokenSource int

const (
	tokenSourceUnknown tokenSource = iota
	tokenSourceSession
	tokenSourceHandshake
)

type ClerkAuth struct {
	enabled     bool
	storage     *users.Storage
	signInURL   string
	signUpURL   string
	signOutURL  string
	jwksClient  *jwks.Client
	jwkCache    *jwkCache
	devInstance bool
}

type jwkCache struct {
	mu   sync.RWMutex
	keys map[string]*clerk.JSONWebKey
}

func (c *jwkCache) get(keyID string) *clerk.JSONWebKey {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.keys[keyID]
}

func (c *jwkCache) set(keyID string, key *clerk.JSONWebKey) {
	if key == nil || keyID == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.keys[keyID] = key
}

func NewClerkAuth(cfg *config.Config, storage *users.Storage) *ClerkAuth {
	auth := &ClerkAuth{
		enabled:     cfg != nil && !cfg.Mocks.Enable && cfg.Clerk.Enabled(),
		storage:     storage,
		signInURL:   cfg.Clerk.SignInURL,
		signUpURL:   cfg.Clerk.SignUpURL,
		signOutURL:  cfg.Clerk.SignOutURL,
		devInstance: isDevInstance(cfg.Clerk.SignInURL, cfg.Clerk.SignUpURL),
		jwkCache: &jwkCache{
			keys: make(map[string]*clerk.JSONWebKey),
		},
	}
	if !auth.enabled {
		return auth
	}

	clerk.SetKey(cfg.Clerk.SecretKey)
	auth.jwksClient = jwks.NewClient(&clerk.ClientConfig{})
	return auth
}

func (a *ClerkAuth) Enabled() bool {
	return a.enabled
}

func (a *ClerkAuth) SignInURL() string {
	return a.signInURL
}

func (a *ClerkAuth) SignUpURL() string {
	return a.signUpURL
}

func (a *ClerkAuth) SignOutURL() string {
	return a.signOutURL
}

func (a *ClerkAuth) Middleware(next http.Handler) http.Handler {
	if !a.enabled {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var token string
		var source tokenSource
		if handshakeToken, ok := a.handshakeTokenFromRequest(r); ok {
			token = handshakeToken
			source = tokenSourceHandshake
		} else {
			token, source = sessionTokenFromRequest(r)
		}
		if token == "" {
			if hasDevBrowserQuery(r) || hasHandshakeQuery(r) {
				http.Redirect(w, r, stripClerkQuery(r), http.StatusSeeOther)
				return
			}
			users.ClearCookie(w)
			next.ServeHTTP(w, r)
			return
		}

		claims, err := a.verifyToken(r.Context(), r, token, source)
		if err != nil {
			slog.WarnContext(r.Context(), "invalid clerk session", "error", err)
			users.ClearCookie(w)
			next.ServeHTTP(w, r)
			return
		}

		currentUser, err := a.ensureUser(r.Context(), claims.Subject)
		if err != nil {
			slog.ErrorContext(r.Context(), "failed to load clerk user", "error", err)
			users.ClearCookie(w)
			next.ServeHTTP(w, r)
			return
		}

		users.SetCookie(w, currentUser.ID, localSessionDuration)
		ctx := users.ContextWithUser(r.Context(), currentUser)
		ctx = clerk.ContextWithSessionClaims(ctx, claims)
		if source == tokenSourceHandshake && hasHandshakeQuery(r) {
			http.Redirect(w, r, stripClerkQuery(r), http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func sessionTokenFromRequest(r *http.Request) (string, tokenSource) {
	if r == nil {
		return "", tokenSourceUnknown
	}
	authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
		return strings.TrimSpace(authHeader[len("bearer "):]), tokenSourceSession
	}
	if cookie, err := r.Cookie(clerkSessionCookie); err == nil && cookie.Value != "" {
		return cookie.Value, tokenSourceSession
	}
	if token := r.URL.Query().Get("__session"); token != "" {
		return token, tokenSourceSession
	}
	return "", tokenSourceUnknown
}

func (a *ClerkAuth) verifyToken(ctx context.Context, r *http.Request, token string, source tokenSource) (*clerk.SessionClaims, error) {
	switch source {
	case tokenSourceHandshake:
		if !a.devInstance || !isLocalHostRequest(r) {
			return nil, errors.New("handshake token not allowed")
		}
		return a.verifySession(ctx, token)
	default:
		return a.verifySession(ctx, token)
	}
}

func (a *ClerkAuth) verifySession(ctx context.Context, token string) (*clerk.SessionClaims, error) {
	if token == "" {
		return nil, errors.New("missing session token")
	}
	unverified, err := jwt.Decode(ctx, &jwt.DecodeParams{Token: token})
	if err != nil {
		return nil, fmt.Errorf("decode session token: %w", err)
	}

	var jwk *clerk.JSONWebKey
	if unverified != nil && unverified.KeyID != "" {
		jwk = a.jwkCache.get(unverified.KeyID)
		if jwk == nil {
			jwk, err = jwt.GetJSONWebKey(ctx, &jwt.GetJSONWebKeyParams{
				KeyID:      unverified.KeyID,
				JWKSClient: a.jwksClient,
			})
			if err != nil {
				return nil, fmt.Errorf("fetch jwk: %w", err)
			}
			a.jwkCache.set(unverified.KeyID, jwk)
		}
	}

	if jwk != nil {
		return jwt.Verify(ctx, &jwt.VerifyParams{Token: token, JWK: jwk})
	}
	return jwt.Verify(ctx, &jwt.VerifyParams{Token: token, JWKSClient: a.jwksClient})
}

func (a *ClerkAuth) ensureUser(ctx context.Context, clerkUserID string) (*users.User, error) {
	if clerkUserID == "" {
		return nil, errors.New("missing clerk user id")
	}
	existing, err := a.storage.GetByID(clerkUserID)
	if err == nil {
		return existing, nil
	}
	if err != nil && !errors.Is(err, users.ErrNotFound) {
		return nil, err
	}

	clerkUser, err := user.Get(ctx, clerkUserID)
	if err != nil {
		return nil, fmt.Errorf("load clerk user: %w", err)
	}
	emails := clerkUserEmails(clerkUser)
	return a.storage.FindOrCreateByClerkUser(clerkUserID, emails)
}

func clerkUserEmails(clerkUser *clerk.User) []string {
	if clerkUser == nil {
		return nil
	}
	emails := make([]string, 0, len(clerkUser.EmailAddresses))
	if clerkUser.PrimaryEmailAddressID != nil {
		for _, addr := range clerkUser.EmailAddresses {
			if addr != nil && addr.ID == *clerkUser.PrimaryEmailAddressID && addr.EmailAddress != "" {
				emails = append(emails, addr.EmailAddress)
				break
			}
		}
	}
	for _, addr := range clerkUser.EmailAddresses {
		if addr == nil || addr.EmailAddress == "" {
			continue
		}
		emails = append(emails, addr.EmailAddress)
	}
	return emails
}

func hasDevBrowserQuery(r *http.Request) bool {
	if r == nil {
		return false
	}
	return r.URL.Query().Has("__clerk_db_jwt")
}

func hasHandshakeQuery(r *http.Request) bool {
	if r == nil {
		return false
	}
	return r.URL.Query().Has("__clerk_handshake")
}

func stripClerkQuery(r *http.Request) string {
	if r == nil {
		return "/"
	}
	u := *r.URL
	q := u.Query()
	q.Del("__clerk_db_jwt")
	q.Del("__clerk_handshake")
	u.RawQuery = q.Encode()
	if u.Path == "" {
		u.Path = "/"
	}
	return u.String()
}

func isDevInstance(signInURL, signUpURL string) bool {
	return strings.Contains(signInURL, ".accounts.dev") || strings.Contains(signUpURL, ".accounts.dev")
}

func isLocalHostRequest(r *http.Request) bool {
	if r == nil {
		return false
	}
	host := strings.ToLower(r.Host)
	if host == "" {
		return false
	}
	if strings.HasPrefix(host, "localhost") || strings.HasPrefix(host, "127.0.0.1") || strings.HasPrefix(host, "[::1]") {
		return true
	}
	return false
}

func (a *ClerkAuth) handshakeTokenFromRequest(r *http.Request) (string, bool) {
	if r == nil {
		return "", false
	}
	handshakeJWT := r.URL.Query().Get("__clerk_handshake")
	if handshakeJWT == "" || !a.devInstance || !isLocalHostRequest(r) {
		return "", false
	}

	unverified, err := jwt.Decode(r.Context(), &jwt.DecodeParams{Token: handshakeJWT})
	if err != nil || unverified == nil {
		return "", false
	}

	rawHandshake, ok := unverified.Extra["handshake"]
	if !ok {
		return "", false
	}

	for _, entry := range handshakeEntries(rawHandshake) {
		if token := extractSessionFromCookie(entry); token != "" {
			return token, true
		}
	}
	return "", false
}

func handshakeEntries(raw any) []string {
	switch v := raw.(type) {
	case []string:
		return v
	case []any:
		entries := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				entries = append(entries, s)
			}
		}
		return entries
	default:
		return nil
	}
}

func extractSessionFromCookie(cookie string) string {
	if cookie == "" {
		return ""
	}
	parts := strings.Split(cookie, ";")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "__session=") {
			value := strings.TrimPrefix(part, "__session=")
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func addRedirectParam(baseURL, redirectURL string) string {
	if baseURL == "" || redirectURL == "" {
		return baseURL
	}
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return baseURL
	}
	q := parsed.Query()
	if q.Get("redirect_url") == "" {
		q.Set("redirect_url", redirectURL)
	}
	parsed.RawQuery = q.Encode()
	return parsed.String()
}
