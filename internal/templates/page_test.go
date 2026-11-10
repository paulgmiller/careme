package templates

import (
	"bytes"
	"context"
	"html/template"
	"testing"

	"careme/internal/logsetup"
	"careme/internal/seasons"

	"github.com/stretchr/testify/require"
)

func TestPageRendersSharedPresentation(t *testing.T) {
	previousClarity, previousGoogle := Clarityproject, GoogleTagManagerID
	t.Cleanup(func() {
		Clarityproject, GoogleTagManagerID = previousClarity, previousGoogle
	})
	for _, enabled := range []bool{true, false} {
		Clarityproject, GoogleTagManagerID = "", ""
		if enabled {
			Clarityproject, GoogleTagManagerID = "project-123", "GTM-ABC123"
		}
		view := struct {
			Page
			Title string
		}{NewPage(logsetup.WithSessionID(context.Background(), "session-123")), "Dinner"}
		require.Equal(t, seasons.GetCurrentStyle(), view.Style)
		tmpl := template.Must(template.New("page").Parse(`{{.Title}}{{.ClarityScript}}{{.GoogleTagScript}}`))
		var output bytes.Buffer
		require.NoError(t, tmpl.Execute(&output, view))
		if enabled {
			require.Contains(t, output.String(), `window.clarity("identify", "session-123", "session-123")`)
			require.Contains(t, output.String(), "GTM-ABC123")
			require.NotContains(t, output.String(), "&lt;script")
		} else {
			require.Equal(t, "Dinner", output.String())
		}
	}
}
