package templates

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"careme/internal/config"
	"careme/internal/logsetup"
	"careme/internal/seasons"

	"golang.org/x/net/html"
)

func TestClarityScriptIncludesSessionID(t *testing.T) {
	prev := Clarityproject
	t.Cleanup(func() {
		Clarityproject = prev
	})
	Clarityproject = "proj-123"

	script := string(ClarityScript(logsetup.WithSessionID(context.Background(), "sess-123")))
	if !strings.Contains(script, `www.clarity.ms/tag/`) {
		t.Fatal("expected clarity script url")
	}
	if !strings.Contains(script, `window.clarity("identify", "sess-123", "sess-123")`) {
		t.Fatalf("expected identify call in script, got %q", script)
	}
}

func TestClarityScriptOmitsIdentifyWhenSessionIDEmpty(t *testing.T) {
	prev := Clarityproject
	t.Cleanup(func() {
		Clarityproject = prev
	})
	Clarityproject = "proj-123"

	script := string(ClarityScript(context.Background()))
	if strings.Contains(script, `window.clarity("identify"`) {
		t.Fatalf("did not expect identify call in script, got %q", script)
	}
}

func TestAboutTemplateRendersValidHTML(t *testing.T) {
	if err := Init(&config.Config{}, "dummyhash.css"); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	data := NewAboutPageData(context.Background(), seasons.GetCurrentStyle())

	var buf bytes.Buffer
	if err := About.Execute(&buf, data); err != nil {
		t.Fatalf("About.Execute() error = %v", err)
	}

	rendered := buf.String()
	if rendered == "" {
		t.Fatal("about page rendered empty HTML")
	}
	if _, err := html.Parse(strings.NewReader(rendered)); err != nil {
		t.Fatalf("about page rendered invalid HTML: %v\nHTML:\n%s", err, rendered)
	}
	if !strings.Contains(rendered, `id="album"`) {
		t.Fatalf("about page should include album section, body: %s", rendered)
	}
	if !strings.Contains(rendered, "Recipe Photo Album") {
		t.Fatalf("about page should include album heading, body: %s", rendered)
	}
	if got := strings.Count(rendered, `data-full="`); got != len(data.AlbumPhotos) {
		t.Fatalf("about page should render %d album photos, got %d", len(data.AlbumPhotos), got)
	}
	wantCaptions := 0
	for _, photo := range data.AlbumPhotos {
		if photo.RecipeHash != "" {
			wantCaptions++
		}
	}
	if got := strings.Count(rendered, `data-caption='`); got != wantCaptions {
		t.Fatalf("about page should render %d recipe captions, got %d", wantCaptions, got)
	}
	if !strings.Contains(rendered, "Dungeness crab pasta") {
		t.Fatalf("about page should render album comments from Go data, body: %s", rendered)
	}
}
