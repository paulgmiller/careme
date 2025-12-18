package admin

import (
	"careme/internal/templates"
	"careme/internal/users"
	"html/template"
	"log/slog"
	"net/http"
)

type handler struct {
	userStorage   *users.Storage
	clarityScript template.HTML
}

// NewHandler creates a new admin HTTP handler
func NewHandler(userStorage *users.Storage, clarityScript template.HTML) *handler {
	return &handler{
		userStorage:   userStorage,
		clarityScript: clarityScript,
	}
}

// Register registers the admin handler routes
func (h *handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("/admin", h.handleAdminPage)
	mux.HandleFunc("/users", h.handleUsersPage)
}

func (h *handler) handleAdminPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	data := struct {
		ClarityScript template.HTML
	}{
		ClarityScript: h.clarityScript,
	}
	if err := templates.Admin.Execute(w, data); err != nil {
		slog.ErrorContext(ctx, "admin template execute error", "error", err)
		http.Error(w, "template error", http.StatusInternalServerError)
	}
}

func (h *handler) handleUsersPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	usersList, err := h.userStorage.List(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "failed to list users", "error", err)
		http.Error(w, "unable to list users", http.StatusInternalServerError)
		return
	}

	data := struct {
		ClarityScript template.HTML
		Users         []users.User
	}{
		ClarityScript: h.clarityScript,
		Users:         usersList,
	}

	if err := templates.Users.Execute(w, data); err != nil {
		slog.ErrorContext(ctx, "users template execute error", "error", err)
		http.Error(w, "template error", http.StatusInternalServerError)
	}
}
