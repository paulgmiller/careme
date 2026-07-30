package admin

import (
	"html/template"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
)

// buildTime is populated by the production build's linker flags.
var buildTime = "unknown"

type pageData struct {
	GitHash   string
	BuildTime string
}

var pageTemplate = template.Must(template.New("admin").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>Admin</title>
</head>
<body>
  <nav>
    <a href="/admin/">Admin</a> |
    <a href="/admin/users">Users</a>
  </nav>
  <h1>Admin</h1>
  <dl>
    <dt>Git hash</dt>
    <dd><code>{{.GitHash}}</code></dd>
    <dt>Build time</dt>
    <dd><time>{{.BuildTime}}</time></dd>
  </dl>
</body>
</html>`))

// Page returns the landing page for the admin area.
func Page() http.Handler {
	return page(pageMetadata())
}

func page(data pageData) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		if err := pageTemplate.Execute(w, data); err != nil {
			slog.ErrorContext(r.Context(), "failed to render admin page", "error", err)
		}
	})
}

func pageMetadata() pageData {
	data := pageData{
		GitHash:   "unknown",
		BuildTime: strings.TrimSpace(buildTime),
	}
	if data.BuildTime == "" {
		data.BuildTime = "unknown"
	}

	info, ok := debug.ReadBuildInfo()
	if !ok {
		return data
	}
	for _, setting := range info.Settings {
		if setting.Key == "vcs.revision" && strings.TrimSpace(setting.Value) != "" {
			data.GitHash = strings.TrimSpace(setting.Value)
			break
		}
	}
	return data
}
