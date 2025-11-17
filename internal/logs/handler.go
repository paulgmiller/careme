package logs

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"os"
	"strconv"
)

// Handler handles HTTP requests for log viewing
type Handler struct {
	reader         *Reader
	clarityScript  template.HTML
	templateLoaded bool
}

// NewHandler creates a new logs HTTP handler
func NewHandler(clarityScript template.HTML) (*Handler, error) {
	// Only create reader if Azure credentials are available
	var reader *Reader
	if accountName := os.Getenv("AZURE_STORAGE_ACCOUNT_NAME"); accountName != "" {
		accountKey := os.Getenv("AZURE_STORAGE_PRIMARY_ACCOUNT_KEY")
		if accountKey != "" {
			var err error
			reader, err = NewReader(context.Background(), Config{
				AccountName: accountName,
				AccountKey:  accountKey,
				Container:   "logs",
			})
			if err != nil {
				return nil, fmt.Errorf("failed to create log reader: %w", err)
			}
		}
	}

	return &Handler{
		reader:        reader,
		clarityScript: clarityScript,
	}, nil
}

// Register registers the log handler routes
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("/logs", h.handleLogsPage)
	mux.HandleFunc("/api/logs", h.handleLogsAPI)
}

// handleLogsPage serves the log viewer HTML page
func (h *Handler) handleLogsPage(w http.ResponseWriter, r *http.Request) {
	if h.reader == nil {
		http.Error(w, "Log viewer is not configured. Set AZURE_STORAGE_ACCOUNT_NAME and AZURE_STORAGE_PRIMARY_ACCOUNT_KEY environment variables.", http.StatusServiceUnavailable)
		return
	}

	// Serve the logs HTML template
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>Log Viewer - Careme</title>
  <script src="https://cdn.tailwindcss.com"></script>
  <script>
    tailwind.config = {
      theme: {
        extend: {
          colors: {
            brand: {
              50: '#fef5ee',
              100: '#fce8d7',
              200: '#f8cdae',
              300: '#f3ab7a',
              400: '#ed7f44',
              500: '#e85e1f',
              600: '#d94715',
              700: '#b43413',
              800: '#902b17',
              900: '#742616',
            }
          }
        }
      }
    }
  </script>
  %s
</head>
<body class="min-h-screen bg-gradient-to-b from-brand-50 to-white antialiased">
  <div class="px-4 py-6">
    <div class="mx-auto max-w-7xl">
      <!-- Header -->
      <div class="mb-6 flex items-center justify-between">
        <div>
          <h1 class="text-3xl font-bold text-brand-700">Log Viewer</h1>
          <p class="mt-1 text-sm text-gray-600">View and filter application logs from Azure Blob Storage</p>
        </div>
        <a href="/" class="rounded-lg bg-brand-600 px-4 py-2 text-sm font-semibold text-white shadow-md transition hover:bg-brand-700">
          Back to Home
        </a>
      </div>

      <!-- Controls -->
      <div class="mb-4 rounded-lg border border-brand-100 bg-white p-4 shadow-sm">
        <div class="flex flex-wrap items-end gap-4">
          <div>
            <label for="hours" class="block text-sm font-medium text-gray-700">Time Range (hours)</label>
            <input type="number" id="hours" value="24" min="1" max="168"
                   class="mt-1 block w-32 rounded-md border-gray-300 shadow-sm focus:border-brand-500 focus:ring-brand-500" />
          </div>
          <div class="flex-1">
            <label for="filter" class="block text-sm font-medium text-gray-700">Filter (search all fields)</label>
            <input type="text" id="filter" placeholder="Search logs..."
                   class="mt-1 block w-full rounded-md border-gray-300 shadow-sm focus:border-brand-500 focus:ring-brand-500" />
          </div>
          <div>
            <label for="level" class="block text-sm font-medium text-gray-700">Level</label>
            <select id="level" class="mt-1 block rounded-md border-gray-300 shadow-sm focus:border-brand-500 focus:ring-brand-500">
              <option value="">All</option>
              <option value="DEBUG">DEBUG</option>
              <option value="INFO">INFO</option>
              <option value="WARN">WARN</option>
              <option value="ERROR">ERROR</option>
            </select>
          </div>
          <button id="refresh" class="rounded-lg bg-brand-600 px-4 py-2 text-sm font-semibold text-white shadow-md transition hover:bg-brand-700">
            Refresh
          </button>
        </div>
      </div>

      <!-- Status -->
      <div id="status" class="mb-4 hidden rounded-lg border p-4">
        <p id="status-text" class="text-sm"></p>
      </div>

      <!-- Logs Display -->
      <div class="overflow-x-auto rounded-lg border border-gray-200 bg-white shadow-sm">
        <div id="loading" class="p-8 text-center text-gray-500">
          Loading logs...
        </div>
        <div id="logs-container" class="hidden">
          <table class="min-w-full divide-y divide-gray-200">
            <thead class="bg-gray-50">
              <tr>
                <th class="px-4 py-3 text-left text-xs font-medium uppercase tracking-wider text-gray-500">Time</th>
                <th class="px-4 py-3 text-left text-xs font-medium uppercase tracking-wider text-gray-500">Level</th>
                <th class="px-4 py-3 text-left text-xs font-medium uppercase tracking-wider text-gray-500">Message</th>
                <th class="px-4 py-3 text-left text-xs font-medium uppercase tracking-wider text-gray-500">Details</th>
              </tr>
            </thead>
            <tbody id="logs-body" class="divide-y divide-gray-200 bg-white">
            </tbody>
          </table>
        </div>
        <div id="no-logs" class="hidden p-8 text-center text-gray-500">
          No logs found for the selected time range and filters.
        </div>
      </div>
    </div>
  </div>

  <script>
    let allLogs = [];
    let filteredLogs = [];

    function formatTime(timeStr) {
      const date = new Date(timeStr);
      return date.toLocaleString();
    }

    function getLevelClass(level) {
      const levels = {
        'ERROR': 'bg-red-100 text-red-800',
        'WARN': 'bg-yellow-100 text-yellow-800',
        'INFO': 'bg-blue-100 text-blue-800',
        'DEBUG': 'bg-gray-100 text-gray-800'
      };
      return levels[level] || 'bg-gray-100 text-gray-800';
    }

    function renderLogs(logs) {
      const tbody = document.getElementById('logs-body');
      const container = document.getElementById('logs-container');
      const noLogs = document.getElementById('no-logs');
      const loading = document.getElementById('loading');

      loading.classList.add('hidden');

      if (logs.length === 0) {
        container.classList.add('hidden');
        noLogs.classList.remove('hidden');
        return;
      }

      noLogs.classList.add('hidden');
      container.classList.remove('hidden');

      tbody.innerHTML = logs.map(log => {
        const details = {...log};
        delete details.time;
        delete details.level;
        delete details.msg;
        
        const detailsStr = Object.keys(details).length > 0 
          ? JSON.stringify(details, null, 2) 
          : '-';

        return '<tr class="hover:bg-gray-50">' +
          '<td class="whitespace-nowrap px-4 py-3 text-sm text-gray-900">' + formatTime(log.time) + '</td>' +
          '<td class="whitespace-nowrap px-4 py-3 text-sm">' +
          '<span class="inline-flex rounded-full px-2 text-xs font-semibold leading-5 ' + getLevelClass(log.level) + '">' +
          log.level +
          '</span>' +
          '</td>' +
          '<td class="px-4 py-3 text-sm text-gray-900">' + escapeHtml(log.msg) + '</td>' +
          '<td class="px-4 py-3 text-sm text-gray-500">' +
          '<details class="cursor-pointer">' +
          '<summary class="text-brand-600 hover:text-brand-700">View</summary>' +
          '<pre class="mt-2 overflow-x-auto rounded bg-gray-50 p-2 text-xs">' + escapeHtml(detailsStr) + '</pre>' +
          '</details>' +
          '</td>' +
          '</tr>';
      }).join('');
    }

    function escapeHtml(text) {
      const div = document.createElement('div');
      div.textContent = text;
      return div.innerHTML;
    }

    function filterLogs() {
      const filterText = document.getElementById('filter').value.toLowerCase();
      const levelFilter = document.getElementById('level').value;

      filteredLogs = allLogs.filter(log => {
        // Level filter
        if (levelFilter && log.level !== levelFilter) {
          return false;
        }

        // Text filter across all fields
        if (filterText) {
          const logStr = JSON.stringify(log).toLowerCase();
          if (!logStr.includes(filterText)) {
            return false;
          }
        }

        return true;
      });

      renderLogs(filteredLogs);
    }

    async function loadLogs() {
      const hours = document.getElementById('hours').value;
      const status = document.getElementById('status');
      const statusText = document.getElementById('status-text');
      const loading = document.getElementById('loading');

      loading.classList.remove('hidden');
      status.classList.add('hidden');

      try {
        const response = await fetch('/api/logs?hours=' + hours);
        if (!response.ok) {
          throw new Error('Failed to fetch logs: ' + response.statusText);
        }

        allLogs = await response.json();
        filterLogs();

        status.classList.remove('hidden');
        status.className = 'mb-4 rounded-lg border border-green-200 bg-green-50 p-4';
        statusText.className = 'text-sm text-green-800';
        statusText.textContent = 'Loaded ' + allLogs.length + ' log entries from the last ' + hours + ' hours';
      } catch (error) {
        console.error('Error loading logs:', error);
        loading.classList.add('hidden');
        status.classList.remove('hidden');
        status.className = 'mb-4 rounded-lg border border-red-200 bg-red-50 p-4';
        statusText.className = 'text-sm text-red-800';
        statusText.textContent = 'Error: ' + error.message;
      }
    }

    // Event listeners
    document.getElementById('refresh').addEventListener('click', loadLogs);
    document.getElementById('filter').addEventListener('input', filterLogs);
    document.getElementById('level').addEventListener('change', filterLogs);

    // Load logs on page load
    loadLogs();
  </script>
</body>
</html>`, h.clarityScript)
}

// handleLogsAPI serves the logs as JSON
func (h *Handler) handleLogsAPI(w http.ResponseWriter, r *http.Request) {
	if h.reader == nil {
		http.Error(w, "Log viewer is not configured", http.StatusServiceUnavailable)
		return
	}

	// Parse hours parameter
	hoursStr := r.URL.Query().Get("hours")
	hours := 24 // default
	if hoursStr != "" {
		if h, err := strconv.Atoi(hoursStr); err == nil && h > 0 {
			hours = h
		}
	}

	// Get logs
	logs, err := h.reader.GetLogs(r.Context(), hours)
	if err != nil {
		slog.ErrorContext(r.Context(), "failed to get logs", "error", err)
		http.Error(w, fmt.Sprintf("Failed to retrieve logs: %v", err), http.StatusInternalServerError)
		return
	}

	// Return as JSON
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(logs); err != nil {
		slog.ErrorContext(r.Context(), "failed to encode logs", "error", err)
	}
}
