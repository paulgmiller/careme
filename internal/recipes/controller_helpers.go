package recipes

import (
	"context"
	"encoding/base64"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"careme/internal/auth"
	"careme/internal/httpx"
	"careme/internal/seasons"
	"careme/internal/templates"
)

func signInPath(returnTo string) string {
	returnTo = strings.TrimSpace(returnTo)
	if returnTo == "" {
		return "/sign-in"
	}
	// We base64-url encode the full relative target so nested query strings survive
	// Clerk's redirect_url handoff without splitting into separate top-level params.
	return "/sign-in?return_to_b64=" + url.QueryEscape(base64.RawURLEncoding.EncodeToString([]byte(returnTo)))
}

func redirectToSignIn(w http.ResponseWriter, r *http.Request, status int) {
	target := signInPath(httpx.RequestPath(r))
	if httpx.IsHTMX(r) {
		w.Header().Set("HX-Redirect", target)
	}
	http.Error(w, "must be logged in", status)
}

func redirectToAccountRequired(w http.ResponseWriter, r *http.Request, reason auth.AccountRequiredReason, returnTo string) {
	target := auth.AccountRequiredPath(reason, returnTo)
	if httpx.IsHTMX(r) {
		w.Header().Set("HX-Redirect", target)
		return
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

type spinnerData struct {
	ClarityScript   template.HTML
	GoogleTagScript template.HTML
	Style           seasons.Style
	RefreshInterval string // seconds
	StatusMessage   string
	ServerSignedIn  bool
	CurrentPath     string
	RetryPath       string
	GenerationError string
}

func newSpinnerData(ctx context.Context) spinnerData {
	return spinnerData{
		ClarityScript:   templates.ClarityScript(ctx),
		GoogleTagScript: templates.GoogleTagScript(),
		Style:           seasons.GetCurrentStyle(),
		ServerSignedIn:  true, // clerk refresh doesn't need to reload because spin will just do it anyways
	}
}

func spin(ctx context.Context, w http.ResponseWriter, r *http.Request, status string) {
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")

	data := newSpinnerData(ctx)
	data.RefreshInterval = "10" // seconds
	data.StatusMessage = status
	data.CurrentPath = r.URL.RequestURI()

	if httpx.IsHTMX(r) {
		if err := templates.Spin.ExecuteTemplate(w, "spin_progress", data); err != nil {
			slog.ErrorContext(ctx, "spin progress template execute error", "error", err)
			http.Error(w, "template error", http.StatusInternalServerError)
		}
		return
	}

	if err := templates.Spin.Execute(w, data); err != nil {
		slog.ErrorContext(ctx, "home template execute error", "error", err)
		http.Error(w, "template error", http.StatusInternalServerError)
	}
}

func (s *server) renderRecipeRegenerationRetry(ctx context.Context, w http.ResponseWriter, r *http.Request, hash string) {
	retryURL := url.URL{Path: "/recipe/" + url.PathEscape(hash) + "/regenerate"}
	renderGenerationRetry(ctx, w, r, retryURL.String(), "")
}

func renderGenerationRetry(ctx context.Context, w http.ResponseWriter, r *http.Request, retryPath, generationError string) {
	data := newSpinnerData(ctx)
	data.RetryPath = retryPath
	data.GenerationError = generationError

	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	if httpx.IsHTMX(r) {
		if err := templates.Spin.ExecuteTemplate(w, "generation_retry", data); err != nil {
			slog.ErrorContext(ctx, "generation retry template execute error", "error", err)
			http.Error(w, "template error", http.StatusInternalServerError)
		}
		return
	}
	if err := templates.Spin.Execute(w, data); err != nil {
		slog.ErrorContext(ctx, "generation retry page execute error", "error", err)
		http.Error(w, "template error", http.StatusInternalServerError)
	}
}

// redirectToHash keeps only query arguments explicitly named by the caller.
func redirectToHash(w http.ResponseWriter, r *http.Request, hash string, argsToKeep ...string) {
	args := url.Values{} // intentionally clear other args
	if slices.Contains(argsToKeep, QueryArgHelp) {
		args.Set(QueryArgHelp, r.URL.Query().Get(QueryArgHelp))
	}
	redirectToHashWithArgs(w, r, hash, args)
}

func redirectToRecipe(w http.ResponseWriter, r *http.Request, hash string) {
	u := url.URL{Path: "/recipe/" + url.PathEscape(hash)}
	if httpx.IsHTMX(r) {
		w.Header().Set("HX-Redirect", u.String())
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, u.String(), http.StatusSeeOther)
}

func redirectToRecipeRegeneration(w http.ResponseWriter, r *http.Request, hash, jobID string) {
	u := url.URL{Path: "/recipe/" + url.PathEscape(hash) + "/regen/" + url.PathEscape(jobID)}
	if httpx.IsHTMX(r) {
		w.Header().Set("HX-Redirect", u.String())
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, u.String(), http.StatusSeeOther)
}

func redirectToHashWithConversion(w http.ResponseWriter, r *http.Request, hash string, event templates.ConversionEvent) {
	args := url.Values{}
	args.Add(queryArgConversion, string(event))
	if help := r.URL.Query().Get(QueryArgHelp); help != "" {
		args.Set(QueryArgHelp, help)
	}
	redirectToHashWithArgs(w, r, hash, args)
}

func redirectToHashWithArgs(w http.ResponseWriter, r *http.Request, hash string, args url.Values) {
	u := url.URL{Path: "/recipes"}
	args.Set(queryArgHash, hash)

	u.RawQuery = args.Encode()
	if httpx.IsHTMX(r) {
		w.Header().Set("HX-Redirect", u.String())
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, u.String(), http.StatusSeeOther)
}
