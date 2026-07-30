package admin

import (
	"embed"
	"html/template"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
)

type pageData struct {
	GitHash    string
	CommitTime string
	GoVersion  string
	DirtyTree  string
}

//go:embed page.html
var pageTemplates embed.FS

var pageTemplate = template.Must(template.ParseFS(pageTemplates, "page.html"))

// Page returns the landing page for the admin area.
func Page() http.Handler {
	data := pageMetadata()
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
		GitHash:    "unknown",
		CommitTime: "unknown",
		GoVersion:  "unknown",
		DirtyTree:  "unknown",
	}

	info, ok := debug.ReadBuildInfo()
	if !ok {
		return data
	}
	if strings.TrimSpace(info.GoVersion) != "" {
		data.GoVersion = strings.TrimSpace(info.GoVersion)
	}
	for _, setting := range info.Settings {
		value := strings.TrimSpace(setting.Value)
		switch setting.Key {
		case "vcs.revision":
			if value == "" {
				continue
			}
			data.GitHash = strings.TrimSpace(setting.Value)
		case "vcs.time":
			if value == "" {
				continue
			}
			data.CommitTime = value
		case "vcs.modified":
			switch value {
			case "true":
				data.DirtyTree = "yes"
			case "false":
				data.DirtyTree = "no"
			}
		}
	}
	return data
}
