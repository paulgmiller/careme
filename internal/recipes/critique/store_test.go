package critique

import (
	"os"
	"path/filepath"
	"testing"

	"careme/internal/ai"
	"careme/internal/cache"
)

func TestStoreSaveUsesPrefixedKey(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	store := NewStore(cache.NewFileCache(tmpDir))

	hash := "recipe-hash"
	want := &ai.RecipeCritique{
		SchemaVersion:  "recipe-critique-v1",
		OverallScore:   8,
		Summary:        "Strong draft.",
		Strengths:      []string{"balanced"},
		Issues:         []ai.RecipeCritiqueIssue{{Severity: "low", Category: "clarity", Detail: "One step could be tighter."}},
		SuggestedFixes: []string{"tighten one step"},
	}

	if err := store.Save(t.Context(), hash, want); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(tmpDir, CachePrefix, hash)); err != nil {
		t.Fatalf("expected recipe critique at prefixed key: %v", err)
	}

	got, err := store.FromCache(t.Context(), hash)
	if err != nil {
		t.Fatalf("FromCache failed: %v", err)
	}
	if got.Summary != want.Summary {
		t.Fatalf("unexpected cached critique: %#v", got)
	}
}

func TestStoreListHashes(t *testing.T) {
	t.Parallel()

	store := NewStore(cache.NewFileCache(t.TempDir()))
	for _, hash := range []string{"b", "a"} {
		if err := store.Save(t.Context(), hash, &ai.RecipeCritique{
			SchemaVersion: "recipe-critique-v1",
			OverallScore:  7,
			Summary:       hash,
		}); err != nil {
			t.Fatalf("Save(%q) failed: %v", hash, err)
		}
	}

	hashes, err := store.ListHashes(t.Context())
	if err != nil {
		t.Fatalf("ListHashes failed: %v", err)
	}

	if len(hashes) != 2 || hashes[0] != "a" || hashes[1] != "b" {
		t.Fatalf("unexpected hashes: %#v", hashes)
	}
}
