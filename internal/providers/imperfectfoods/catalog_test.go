package imperfectfoods

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSnapshotDeduplicatesAndClassifiesProducts(t *testing.T) {
	var source snapshot
	require.NoError(t, json.Unmarshal(snapshotJSON, &source))
	food := source.Aisles[0].Products[0]
	for i := range source.Aisles {
		if source.Aisles[i].Name != "Wine Shop" {
			source.Aisles[i].Products = []snapshotProduct{food}
		}
	}
	raw, err := json.Marshal(source)
	require.NoError(t, err)
	got, err := parseSnapshot(raw)
	require.NoError(t, err)
	require.Len(t, got.staples, 1)
	assert.Contains(t, got.staples[0].Categories, "Vegetables")
	assert.Contains(t, got.staples[0].Categories, "Rescued")
	assert.Contains(t, got.staples[0].Categories, "Pantry")
	assert.NotEmpty(t, got.wines)
}

func TestSnapshotRejectsIncompleteOrConflictingData(t *testing.T) {
	for _, tt := range []struct {
		name   string
		change func(*snapshot)
		want   string
	}{
		{"uncaptured", func(s *snapshot) { s.CapturedAt = time.Time{} }, "has not been captured"},
		{"missing aisle", func(s *snapshot) { s.Aisles = s.Aisles[1:] }, "needs all"},
		{"empty aisle", func(s *snapshot) { s.Aisles[0].Products = nil }, "no captured products"},
		{"wrong URL", func(s *snapshot) { s.Aisles[0].URL = "https://example.com" }, "unexpected or duplicate"},
		{"duplicate aisle", func(s *snapshot) { s.Aisles[1] = s.Aisles[0] }, "unexpected or duplicate"},
		{"missing identity", func(s *snapshot) { s.Aisles[0].Products[0].ProductID = "" }, "needs an ID or URL"},
		{"missing name", func(s *snapshot) { s.Aisles[0].Products[0].Description = "" }, "needs an ID or URL"},
		{"unknown kind", func(s *snapshot) { s.Aisles[0].Products[0].Kind = "unknown" }, "invalid kind"},
		{"conflicting kind", func(s *snapshot) {
			p := s.Aisles[0].Products[0]
			p.Kind = "wine"
			s.Aisles[1].Products = append(s.Aisles[1].Products, p)
		}, "conflicting food/wine"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var s snapshot
			require.NoError(t, json.Unmarshal(snapshotJSON, &s))
			tt.change(&s)
			raw, err := json.Marshal(s)
			require.NoError(t, err)
			_, err = parseSnapshot(raw)
			require.ErrorContains(t, err, tt.want)
		})
	}
	_, err := parseSnapshot([]byte("{"))
	require.ErrorContains(t, err, "decode snapshot")
}

func TestSnapshotChangesInvalidateCache(t *testing.T) {
	date := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	first := snapshotHash(locationID, date, snapshotJSON).Sum(nil)
	assert.NotEqual(t, first, snapshotHash(locationID, date, []byte("changed catalog")).Sum(nil))
	assert.Equal(t, first, snapshotHash(locationID, date.Add(time.Hour), snapshotJSON).Sum(nil))
	assert.NotEqual(t, first, snapshotHash(locationID, date.AddDate(0, 0, 1), snapshotJSON).Sum(nil))
}
