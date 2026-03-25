`internal/brightdata` contains a small Go client for Bright Data scrapers.

Example:

```go
package main

import (
	"context"
	"log"
	"time"

	"careme/internal/brightdata"
)

type WalmartInput struct {
	URL     string `json:"url"`
	ZipCode string `json:"zip_code"`
	StoreID string `json:"store_id"`
}

func main() {
	client, err := brightdata.NewClient("YOUR_API_KEY", nil)
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()
	input := []WalmartInput{
		{
			URL:     "https://www.walmart.com/ip/5332753715",
			ZipCode: "33177",
			StoreID: "6397",
		},
	}

	resp, err := client.Scrape(ctx, "gd_m693oc1r1gebnayxq", input, brightdata.ScrapeOptions{
		IncludeErrors: true,
		Format:        brightdata.FormatJSON,
	})
	if err != nil {
		log.Fatal(err)
	}

	if resp.SnapshotID != "" {
		result, err := client.WaitAndDownload(ctx, resp.SnapshotID, 2*time.Second, brightdata.DownloadOptions{
			Format: brightdata.FormatJSON,
		})
		if err != nil {
			log.Fatal(err)
		}
		log.Printf("downloaded %d bytes", len(result.Body))
		return
	}

	log.Printf("sync response: %s", string(resp.Body))
}
```

Relevant methods:

- `Scrape`: synchronous request that may fall back to a `snapshot_id` on HTTP `202`
- `Trigger`: always asynchronous, returns a `snapshot_id`
- `Progress` and `WaitForSnapshot`: polling helpers
- `DownloadSnapshot`: fetch a completed snapshot body
- `DeliverToAzure`: tell Bright Data to write a completed snapshot into Azure Blob Storage
