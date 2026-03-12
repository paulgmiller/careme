package logs

import (
	"careme/internal/logsink"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
)

type handler struct {
	reader    *Reader
	datasette http.Handler
}

func NewHandler(cfg logsink.Config) (*handler, error) {
	reader, err := NewReader(&cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create log reader: %w", err)
	}

	datasetteProxy, err := newDatasetteProxy()
	if err != nil {
		return nil, fmt.Errorf("failed to create datasette proxy: %w", err)
	}

	return &handler{
		reader:    reader,
		datasette: datasetteProxy,
	}, nil
}

// Register registers the log handler routes
func (h *handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("/logs", h.handleLogsPage)
	mux.HandleFunc("/api/logs", h.handleLogsAPI)
	mux.Handle("/datasette/", http.StripPrefix("/datasette", h.datasette))
}

func (h *handler) handleLogsPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy",
		"default-src 'none'; "+
			"script-src 'unsafe-inline'; "+
			"base-uri 'none'; "+
			"form-action 'none'; "+
			"frame-ancestors 'none'; "+
			"upgrade-insecure-requests;")

	page := `<!doctype html>
<meta charset="utf-8" />
<title>Logs</title>
<script>
  const api = new URL("/admin/api/logs", location.origin);
  const qs = new URLSearchParams(location.search);
  for (const k of ["hours"]) if (qs.has(k)) api.searchParams.set(k, qs.get(k));

  const lite = new URL("/admin/datasette/", location.origin);
  lite.searchParams.set("json", api.toString());

  location.replace(lite.toString());
</script>`

	_, err := w.Write([]byte(page))
	if err != nil {
		slog.ErrorContext(r.Context(), "failed to write logs page", "error", err)
	}
}

func (h *handler) handleLogsAPI(w http.ResponseWriter, r *http.Request) {
	hoursStr := r.URL.Query().Get("hours")
	hours := 24
	if hoursStr != "" {
		if parsedHours, err := strconv.Atoi(hoursStr); err == nil && parsedHours > 0 {
			hours = parsedHours
		}
	}

	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")
	err := h.reader.GetLogs(r.Context(), hours, w)
	if err != nil {
		slog.ErrorContext(r.Context(), "failed to get logs", "error", err)
		http.Error(w, fmt.Sprintf("Failed to retrieve logs: %v", err), http.StatusInternalServerError)
		return
	}
}

func newDatasetteProxy() (http.Handler, error) {
	target, err := url.Parse("https://lite.datasette.io")
	if err != nil {
		return nil, err
	}

	proxy := newSingleHostProxy(target)
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		slog.ErrorContext(r.Context(), "datasette proxy request failed", "error", err)
		http.Error(w, "Datasette is unavailable", http.StatusBadGateway)
	}

	return proxy, nil
}

func newSingleHostProxy(target *url.URL) *httputil.ReverseProxy {
	proxy := httputil.NewSingleHostReverseProxy(target)
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		// Ensure upstream host routing matches the target domain.
		req.Host = target.Host
	}
	return proxy
}
