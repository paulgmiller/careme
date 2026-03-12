package main

import (
	"careme/internal/albertsons"
	"context"
	"encoding/json"
	"flag"
	"log"
	"os"
	"time"
)

func main() {
	var (
		pageURL      string
		storeID      string
		visitorID    string
		uuid         string
		banner       string
		cookieHeader string
		rows         int
	)

	flag.StringVar(&pageURL, "page-url", "https://www.safeway.com/aisle-vs/meat-seafood/meat-favorites.html", "Safeway curated aisle page URL")
	flag.StringVar(&storeID, "store-id", "490", "Albertsons-family store id")
	flag.StringVar(&visitorID, "visitor-id", os.Getenv("SAFEWAY_VISITOR_ID"), "visitor id header/query value")
	flag.StringVar(&uuid, "uuid", os.Getenv("SAFEWAY_UUID"), "session uuid header/query value")
	flag.StringVar(&banner, "banner", "safeway", "banner name")
	flag.StringVar(&cookieHeader, "cookie-header", os.Getenv("SAFEWAY_COOKIE_HEADER"), "raw Cookie header to send to Safeway")
	flag.IntVar(&rows, "rows", 120, "pathway page size; Safeway currently caps this at 120")
	flag.Parse()

	client := albertsons.NewPathwayClient(nil)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	products, err := client.FetchCuratedProducts(ctx, pageURL, albertsons.FetchCuratedProductsOptions{
		StoreID:      storeID,
		VisitorID:    visitorID,
		UUID:         uuid,
		Banner:       banner,
		CookieHeader: cookieHeader,
		Rows:         rows,
	})
	if err != nil {
		log.Fatalf("fetch curated products: %v", err)
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(products); err != nil {
		log.Fatalf("encode products: %v", err)
	}
}
