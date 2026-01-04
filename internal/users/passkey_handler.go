package users

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

type passkeyHandler struct {
	storage  *Storage
	sessions *sessionStore
}

type sessionStore struct {
	mu       sync.Mutex
	sessions map[string]passkeySession
}

type passkeySession struct {
	UserID string
	Data   webauthn.SessionData
}

type emailRequest struct {
	Email string `json:"email"`
}

func newSessionStore() *sessionStore {
	return &sessionStore{sessions: make(map[string]passkeySession)}
}

func (s *sessionStore) Save(data passkeySession) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := protocol.CreateChallenge()
	s.sessions[id] = data
	return id
}

func (s *sessionStore) Get(id string) (passkeySession, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, ok := s.sessions[id]
	return data, ok
}

func (s *sessionStore) Delete(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, id)
}

func NewPasskeyHandler(storage *Storage) *passkeyHandler {
	return &passkeyHandler{storage: storage, sessions: newSessionStore()}
}

func (h *passkeyHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /passkeys/register/options", h.beginRegistration)
	mux.HandleFunc("POST /passkeys/register/finish", h.finishRegistration)
	mux.HandleFunc("POST /passkeys/login/options", h.beginLogin)
	mux.HandleFunc("POST /passkeys/login/finish", h.finishLogin)
}

func decodeEmailRequest(r *http.Request) (string, error) {
	var req emailRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return "", fmt.Errorf("invalid request payload: %w", err)
	}
	email := strings.TrimSpace(req.Email)
	if email == "" {
		return "", errors.New("email is required")
	}
	return email, nil
}

func (h *passkeyHandler) beginRegistration(w http.ResponseWriter, r *http.Request) {
	email, err := decodeEmailRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	user, err := h.storage.FindOrCreateByEmail(email)
	if err != nil {
		slog.ErrorContext(r.Context(), "failed to prepare passkey user", "error", err)
		http.Error(w, "unable to start registration", http.StatusInternalServerError)
		return
	}

	webAuthn, err := h.newWebAuthn(r)
	if err != nil {
		slog.ErrorContext(r.Context(), "failed to configure webauthn", "error", err)
		http.Error(w, "passkeys unavailable for host", http.StatusBadRequest)
		return
	}

	options, sessionData, err := webAuthn.BeginRegistration(
		newWebAuthnUser(user),
		webauthn.WithAuthenticatorSelection(protocol.AuthenticatorSelection{
			ResidentKey:      protocol.ResidentKeyRequirementPreferred,
			UserVerification: protocol.VerificationRequired,
		}),
		webauthn.WithConveyancePreference(protocol.PreferNoAttestation),
	)
	if err != nil {
		slog.ErrorContext(r.Context(), "failed to build registration options", "error", err)
		http.Error(w, "unable to start registration", http.StatusInternalServerError)
		return
	}

	h.writeOptionsResponse(w, h.sessions.Save(passkeySession{UserID: user.ID, Data: *sessionData}), options)
}

func (h *passkeyHandler) finishRegistration(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session")
	session, ok := h.sessions.Get(sessionID)
	if !ok {
		http.Error(w, "registration session expired", http.StatusBadRequest)
		return
	}
	defer h.sessions.Delete(sessionID)

	user, err := h.storage.GetByID(session.UserID)
	if err != nil {
		slog.ErrorContext(r.Context(), "user not found for registration", "error", err)
		http.Error(w, "unable to complete registration", http.StatusBadRequest)
		return
	}

	webAuthn, err := h.newWebAuthn(r)
	if err != nil {
		slog.ErrorContext(r.Context(), "failed to configure webauthn", "error", err)
		http.Error(w, "passkeys unavailable for host", http.StatusBadRequest)
		return
	}

	credential, err := webAuthn.FinishRegistration(newWebAuthnUser(user), session.Data, r)
	if err != nil {
		slog.WarnContext(r.Context(), "passkey registration failed", "error", err)
		http.Error(w, "passkey validation failed", http.StatusBadRequest)
		return
	}

	ReplacePasskeyFromCredential(user, credential)
	if err := h.storage.Update(user); err != nil {
		slog.ErrorContext(r.Context(), "failed to persist passkey", "error", err)
		http.Error(w, "unable to save passkey", http.StatusInternalServerError)
		return
	}

	SetCookie(w, user.ID, SessionDuration)
	h.writeSuccessResponse(w)
}

func (h *passkeyHandler) beginLogin(w http.ResponseWriter, r *http.Request) {
	email, err := decodeEmailRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	user, err := h.storage.GetByEmail(email)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			http.Error(w, "no account found for that email", http.StatusBadRequest)
			return
		}
		slog.ErrorContext(r.Context(), "failed to lookup user for login", "error", err)
		http.Error(w, "unable to start login", http.StatusInternalServerError)
		return
	}

	if len(user.Passkeys) == 0 {
		http.Error(w, "create a passkey first", http.StatusBadRequest)
		return
	}

	webAuthn, err := h.newWebAuthn(r)
	if err != nil {
		slog.ErrorContext(r.Context(), "failed to configure webauthn", "error", err)
		http.Error(w, "passkeys unavailable for host", http.StatusBadRequest)
		return
	}

	options, sessionData, err := webAuthn.BeginLogin(newWebAuthnUser(user), webauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		slog.ErrorContext(r.Context(), "failed to build login options", "error", err)
		http.Error(w, "unable to start login", http.StatusInternalServerError)
		return
	}

	h.writeOptionsResponse(w, h.sessions.Save(passkeySession{UserID: user.ID, Data: *sessionData}), options)
}

func (h *passkeyHandler) finishLogin(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session")
	session, ok := h.sessions.Get(sessionID)
	if !ok {
		http.Error(w, "login session expired", http.StatusBadRequest)
		return
	}
	defer h.sessions.Delete(sessionID)

	user, err := h.storage.GetByID(session.UserID)
	if err != nil {
		slog.ErrorContext(r.Context(), "user not found for login", "error", err)
		http.Error(w, "unable to complete login", http.StatusBadRequest)
		return
	}

	webAuthn, err := h.newWebAuthn(r)
	if err != nil {
		slog.ErrorContext(r.Context(), "failed to configure webauthn", "error", err)
		http.Error(w, "passkeys unavailable for host", http.StatusBadRequest)
		return
	}

	credential, err := webAuthn.FinishLogin(newWebAuthnUser(user), session.Data, r)
	if err != nil {
		slog.WarnContext(r.Context(), "passkey verification failed", "error", err)
		http.Error(w, "login failed", http.StatusUnauthorized)
		return
	}

	UpdatePasskeyFromCredential(user, credential)
	if err := h.storage.Update(user); err != nil {
		slog.ErrorContext(r.Context(), "failed to update passkey usage", "error", err)
		http.Error(w, "unable to finalize login", http.StatusInternalServerError)
		return
	}

	SetCookie(w, user.ID, SessionDuration)
	h.writeSuccessResponse(w)
}

func (h *passkeyHandler) writeOptionsResponse(w http.ResponseWriter, sessionID string, options any) {
	payload := struct {
		SessionID string      `json:"session_id"`
		Options   interface{} `json:"options"`
	}{sessionID, options}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(payload)
}

func (h *passkeyHandler) writeSuccessResponse(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"redirect": "/"})
}

func (h *passkeyHandler) newWebAuthn(r *http.Request) (*webauthn.WebAuthn, error) {
	host := r.Host
	if idx := strings.Index(host, ":"); idx != -1 {
		host = host[:idx]
	}
	if host == "" {
		return nil, errors.New("missing host for webauthn")
	}
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}

	return webauthn.New(&webauthn.Config{
		RPDisplayName: "Careme",
		RPID:          host,
		RPOrigin:      fmt.Sprintf("%s://%s", scheme, r.Host),
	})
}
