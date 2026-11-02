package mnfoodclubstub

import (
	"encoding/base64"
	"errors"
	"html/template"
	"log/slog"
	"net/http"
	"strings"
	"unicode/utf8"

	ordertypes "careme/internal/orders/types"
)

const loginCookieName = "mnfoodclub_stub_name"

func validName(name string) bool {
	return name != "" && utf8.ValidString(name) && utf8.RuneCountInString(name) <= 100
}

func loginName(r *http.Request) string {
	cookie, err := r.Cookie(loginCookieName)
	if err != nil {
		return ""
	}
	decoded, err := base64.RawURLEncoding.DecodeString(cookie.Value)
	name := strings.TrimSpace(string(decoded))
	if err != nil || !validName(name) {
		return ""
	}
	return name
}

func (s *server) appliedResponse(id, name string, input request) response {
	result := response{Transfer: ordertypes.Transfer{TransferID: id, Status: ordertypes.TransferApplied, CartURL: s.publicURL + "/#" + id}, UserID: name}
	for _, item := range input.Items {
		result.Items = append(result.Items, ordertypes.TransferItem{ProductID: item.ProductID, AcceptedQuantity: *item.Quantity})
	}
	return result
}

func (s *server) handoffRecord(w http.ResponseWriter, r *http.Request) (record, bool) {
	rec, err := s.store.get(r.Context(), r.PathValue("id"))
	if errors.Is(err, errNotFound) {
		http.NotFound(w, r)
		return record{}, false
	}
	if err != nil {
		slog.ErrorContext(r.Context(), "load stub handoff", "error", err)
		http.Error(w, "Unable to load your test transfer.", 500)
		return record{}, false
	}
	return rec, true
}

func (s *server) applyHandoff(w http.ResponseWriter, r *http.Request, rec record, name string) (response, bool) {
	applied, err := s.store.apply(r.Context(), rec.Response.TransferID, s.appliedResponse(rec.Response.TransferID, name, rec.Request))
	if err != nil {
		slog.ErrorContext(r.Context(), "apply stub handoff", "error", err)
		http.Error(w, "Unable to apply your test transfer.", 500)
		return response{}, false
	}
	return applied.Response, true
}

func (s *server) handoff(w http.ResponseWriter, r *http.Request) {
	rec, ok := s.handoffRecord(w, r)
	if !ok {
		return
	}
	if rec.Response.Status == ordertypes.TransferApplied {
		http.Redirect(w, r, rec.Response.CartURL, http.StatusSeeOther)
		return
	}
	name := loginName(r)
	if name == "" {
		s.loginPage(w, r, "", http.StatusOK)
		return
	}
	result, ok := s.applyHandoff(w, r, rec, name)
	if !ok {
		return
	}
	http.Redirect(w, r, result.CartURL, http.StatusSeeOther)
}

func (s *server) login(w http.ResponseWriter, r *http.Request) {
	rec, ok := s.handoffRecord(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid test login.", 400)
		return
	}
	name := strings.TrimSpace(r.PostForm.Get("name"))
	if !validName(name) {
		s.loginPage(w, r, "Enter a name of up to 100 characters.", http.StatusBadRequest)
		return
	}
	result, ok := s.applyHandoff(w, r, rec, name)
	if !ok {
		return
	}
	http.SetCookie(w, &http.Cookie{Name: loginCookieName, Value: base64.RawURLEncoding.EncodeToString([]byte(name)), Path: s.cookiePath, HttpOnly: true, Secure: s.secureCookie, SameSite: http.SameSiteLaxMode, MaxAge: 30 * 24 * 60 * 60})
	http.Redirect(w, r, result.CartURL, http.StatusSeeOther)
}

var loginTemplate = template.Must(template.New("login").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>Food Club test login</title></head>
<body><main><h1>Food Club test login</h1><p>This is a test login. Enter any test name to add this transfer to your cart.</p>
{{if .Error}}<p role="alert">{{.Error}}</p>{{end}}
<form method="POST" action="{{.Action}}"><label for="name">Name</label> <input id="name" name="name" autocomplete="nickname" maxlength="100" required> <button type="submit">Continue to cart</button></form>
</main></body></html>`))

func (s *server) loginPage(w http.ResponseWriter, r *http.Request, message string, status int) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := loginTemplate.Execute(w, struct{ Action, Error string }{s.cookiePath + "handoff/" + r.PathValue("id"), message}); err != nil {
		slog.ErrorContext(r.Context(), "render stub login", "error", err)
	}
}
