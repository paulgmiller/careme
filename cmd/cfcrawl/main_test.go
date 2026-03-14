package main

import (
	"careme/internal/browserrendering"
	"encoding/json"
	"testing"
)

func TestSummarizeCrawlFlattensProducts(t *testing.T) {
	t.Parallel()

	job := &browserrendering.CrawlJob{
		ID:     "job-123",
		Status: "completed",
		Records: []browserrendering.CrawlRecord{
			{
				URL:    "https://example.com/a",
				Status: "completed",
				Metadata: browserrendering.CrawlMetadata{
					Status: 200,
					Title:  "Page A",
				},
				JSON: json.RawMessage(`{"products":[{"description":"Bananas","brand":"Store Brand","regularPrice":1.99}]}`),
			},
			{
				URL:    "https://example.com/b",
				Status: "completed",
				Metadata: browserrendering.CrawlMetadata{
					Status: 200,
					Title:  "Page B",
				},
				JSON: json.RawMessage(`{"products":[{"description":"Carrots","salePrice":0.99},{"description":"Broccoli"}]}`),
			},
		},
	}

	got := summarizeCrawl(job)
	if got.JobID != "job-123" {
		t.Fatalf("unexpected job id: %q", got.JobID)
	}
	if len(got.Pages) != 2 {
		t.Fatalf("unexpected page count: %d", len(got.Pages))
	}
	if got.Pages[0].ProductCount != 1 {
		t.Fatalf("unexpected first page product count: %d", got.Pages[0].ProductCount)
	}
	if got.Pages[1].ProductCount != 2 {
		t.Fatalf("unexpected second page product count: %d", got.Pages[1].ProductCount)
	}
	if len(got.Products) != 3 {
		t.Fatalf("unexpected flattened product count: %d", len(got.Products))
	}
	if got.Products[0].SourceURL != "https://example.com/a" {
		t.Fatalf("unexpected source URL: %q", got.Products[0].SourceURL)
	}
	if got.Products[2].Description != "Broccoli" {
		t.Fatalf("unexpected final product description: %q", got.Products[2].Description)
	}
}

func TestSplitCSV(t *testing.T) {
	t.Parallel()

	got := splitCSV(" one, two ,,three ")
	want := []string{"one", "two", "three"}
	if len(got) != len(want) {
		t.Fatalf("unexpected length: got %d want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("unexpected value at %d: got %q want %q", i, got[i], want[i])
		}
	}
}
