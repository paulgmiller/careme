// TODO merge with log sink
package logs

import (
	"careme/internal/logsink"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
)

// Handler handles HTTP requests for log viewing
type handler struct {
	reader *Reader
}

// NewHandler creates   a new logs HTTP handler
func NewHandler(cfg logsink.Config) (*handler, error) {
	// Only create reader if Azure credentials are available
	reader, err := NewReader(context.Background(), &cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create log reader: %w", err)
	}

	return &handler{
		reader: reader,
	}, nil
}

// Register registers the log handler routes
func (h *handler) Register(mux *http.ServeMux) {
	//mux.HandleFunc("/logs", h.handleLogsPage)
	mux.HandleFunc("/api/logs", h.handleLogsAPI)
}

// handleLogsAPI serves the logs as JSON
func (h *handler) handleLogsAPI(w http.ResponseWriter, r *http.Request) {

	// Parse hours parameter
	hoursStr := r.URL.Query().Get("hours")
	hours := 24 // default
	if hoursStr != "" {
		if h, err := strconv.Atoi(hoursStr); err == nil && h > 0 {
			hours = h
		}
	}

	// Get logs
	err := h.reader.GetLogs(r.Context(), hours, w)
	if err != nil {
		slog.ErrorContext(r.Context(), "failed to get logs", "error", err)
		http.Error(w, fmt.Sprintf("Failed to retrieve logs: %v", err), http.StatusInternalServerError)
		return
	}

	// Return as JSON // kinda?
	w.Header().Set("Content-Type", "application/json")
}
