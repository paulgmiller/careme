package logs

import (
	"careme/internal/logsink"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
)

type handler struct {
	reader *Reader
}

func NewHandler(cfg logsink.Config) (*handler, error) {
	reader, err := NewReader(&cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create log reader: %w", err)
	}

	return &handler{
		reader: reader,
	}, nil
}

func (h *handler) Register(mux *http.ServeMux) {
	mux.Handle("/logs", http.HandlerFunc(h.handleLogsPage))
	mux.Handle("/api/logs", http.HandlerFunc(h.handleLogsAPI))
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

	_, err := w.Write([]byte(`<!doctype html>
<meta charset="utf-8" />
<title>Logs</title>
<script>
  const api = new URL("admin/api/logs", location.href);
  const qs = new URLSearchParams(location.search);
  for (const k of ["hours"]) if (qs.has(k)) api.searchParams.set(k, qs.get(k));

  const lite = new URL("https://lite.datasette.io/");
  lite.searchParams.set("json", api.toString());
  // Optional: turn off analytics
  // lite.searchParams.set("analytics", "off");

  location.replace(lite.toString());
</script>`))
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
