package auth

import (
	"careme/internal/users"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/sendgrid/rest"
	"github.com/sendgrid/sendgrid-go"
	"github.com/sendgrid/sendgrid-go/helpers/mail"
)

const sessionDuration = 365 * 24 * time.Hour

type emailClient interface {
	Send(message *mail.SGMailV3) (*rest.Response, error)
}

// Handler manages authentication endpoints
type Handler struct {
	tokenStorage *TokenStorage
	userStorage  *users.Storage
	emailClient  emailClient
	baseURL      string
}

func NewHandler(tokenStorage *TokenStorage, userStorage *users.Storage, sendGridAPIKey, baseURL string) *Handler {
	return &Handler{
		tokenStorage: tokenStorage,
		userStorage:  userStorage,
		emailClient:  sendgrid.NewSendClient(sendGridAPIKey),
		baseURL:      baseURL,
	}
}

// Register adds authentication routes to the mux
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("/login/request", h.handleLoginRequest)
	mux.HandleFunc("/login/verify", h.handleLoginVerify)
}

// handleLoginRequest generates a magic link and sends it via email
func (h *Handler) handleLoginRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()

	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form submission", http.StatusBadRequest)
		return
	}

	email := strings.TrimSpace(r.FormValue("email"))
	if email == "" {
		http.Error(w, "email is required", http.StatusBadRequest)
		return
	}

	// Generate token
	token, err := h.tokenStorage.GenerateToken(ctx, email)
	if err != nil {
		slog.ErrorContext(ctx, "failed to generate token", "error", err)
		http.Error(w, "unable to generate login link", http.StatusInternalServerError)
		return
	}

	// Send email with magic link
	message := BuildMagicLinkEmail(email, token, h.baseURL)
	response, err := h.emailClient.Send(message)
	if err != nil {
		slog.ErrorContext(ctx, "failed to send magic link email", "error", err, "email", email)
		http.Error(w, "unable to send login email", http.StatusInternalServerError)
		return
	}

	if response.StatusCode >= 400 {
		slog.ErrorContext(ctx, "sendgrid returned error", "status", response.StatusCode, "body", response.Body)
		http.Error(w, "unable to send login email", http.StatusInternalServerError)
		return
	}

	slog.InfoContext(ctx, "magic link sent", "email", email, "status", response.StatusCode)

	// Show success page
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `
<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>Check your email - Careme</title>
  <style>
    body { 
      font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif;
      background: linear-gradient(to bottom, #f0f9ff, #ffffff);
      margin: 0;
      padding: 40px 20px;
      min-height: 100vh;
      display: flex;
      align-items: center;
      justify-content: center;
    }
    .container {
      max-width: 500px;
      background: white;
      padding: 40px;
      border-radius: 12px;
      box-shadow: 0 4px 12px rgba(0,0,0,0.1);
      text-align: center;
    }
    h1 { color: #4F46E5; margin-top: 0; }
    p { color: #666; line-height: 1.6; }
    .email { font-weight: 600; color: #333; }
    a { color: #4F46E5; text-decoration: none; }
    a:hover { text-decoration: underline; }
  </style>
</head>
<body>
  <div class="container">
    <h1>📧 Check your email</h1>
    <p>We've sent a login link to <span class="email">%s</span></p>
    <p>Click the link in the email to sign in. The link will expire in 15 minutes.</p>
    <p style="margin-top: 30px; font-size: 14px;">
      <a href="/">← Back to home</a>
    </p>
  </div>
</body>
</html>
`, email)
}

// handleLoginVerify validates the token and logs the user in
func (h *Handler) handleLoginVerify(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	token := r.URL.Query().Get("token")
	if token == "" {
		http.Error(w, "token is required", http.StatusBadRequest)
		return
	}

	// Validate token and get email
	email, err := h.tokenStorage.ValidateToken(ctx, token)
	if err != nil {
		slog.ErrorContext(ctx, "failed to validate token", "error", err)
		http.Error(w, "invalid or expired login link", http.StatusUnauthorized)
		return
	}

	// Find or create user
	user, err := h.userStorage.FindOrCreateByEmail(email)
	if err != nil {
		slog.ErrorContext(ctx, "failed to find or create user", "error", err, "email", email)
		http.Error(w, "unable to complete sign in", http.StatusInternalServerError)
		return
	}

	// Set session cookie
	users.SetCookie(w, user.ID, sessionDuration)

	slog.InfoContext(ctx, "user logged in via magic link", "email", email, "user_id", user.ID)

	// Redirect to home
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// MockEmailClient is a test implementation that doesn't send real emails
type MockEmailClient struct{}

func (m *MockEmailClient) Send(message *mail.SGMailV3) (*rest.Response, error) {
	return &rest.Response{StatusCode: 202}, nil
}

// NewMockHandler creates a handler with a mock email client for testing
func NewMockHandler(tokenStorage *TokenStorage, userStorage *users.Storage, baseURL string) *Handler {
	return &Handler{
		tokenStorage: tokenStorage,
		userStorage:  userStorage,
		emailClient:  &MockEmailClient{},
		baseURL:      baseURL,
	}
}
