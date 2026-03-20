package safewayads

import (
	"testing"
	"time"
)

func TestCanonicalStoreCode(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "001", want: "1"},
		{in: "490", want: "490"},
		{in: "000", want: "0"},
		{in: "ABC", want: "ABC"},
	}

	for _, tc := range tests {
		if got := CanonicalStoreCode(tc.in); got != tc.want {
			t.Fatalf("CanonicalStoreCode(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSelectWeeklyAd(t *testing.T) {
	publications := []Publication{
		{ID: 1, ExternalDisplayName: "Big Book of Savings"},
		{ID: 2, ExternalDisplayName: "Weekly Ad"},
	}

	got, ok := SelectWeeklyAd(publications)
	if !ok {
		t.Fatal("expected publication")
	}
	if got.ID != 2 {
		t.Fatalf("expected weekly ad publication, got %d", got.ID)
	}
}

func TestFirstPageImageURL(t *testing.T) {
	publication := Publication{
		FirstPageThumbnail2000URL: "https://example.com/2000.jpg",
		FirstPageThumbnailURL:     "https://example.com/xlarge.jpg",
	}
	if got := FirstPageImageURL(publication); got != publication.FirstPageThumbnail2000URL {
		t.Fatalf("expected 2000h url, got %q", got)
	}
}

func TestPageImageKey(t *testing.T) {
	now := time.Date(2026, 3, 5, 12, 0, 0, 0, time.UTC)
	got := PageImageKey("490", 7811393, 3, "https://f.wishabi.net/flyers/7811393/first_page_thumbnail_400w/1772206088.jpg", "image/jpeg", nil, now)
	want := "safeway/weeklyads/images/490/2026-03-05_7811393_p03.jpg"
	if got != want {
		t.Fatalf("PageImageKey() = %q, want %q", got, want)
	}
}

func TestIsInvalidStoreResponse(t *testing.T) {
	body := []byte(`{"message":"Invalid store_code","code":"422"}`)
	if !isInvalidStoreResponse(422, body) {
		t.Fatal("expected invalid store response to be recognized")
	}
	if isInvalidStoreResponse(500, body) {
		t.Fatal("unexpected invalid store response match for non-422 status")
	}
}
