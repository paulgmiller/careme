package main

import (
	"context"
	"flag"
	"fmt"
	"log"

	"careme/internal/config"
	"careme/internal/recipes"
)

func main() {
	var location string
	var verbose bool
	var style string
	flag.StringVar(&location, "location", "", "Location for recipe sourcing (e.g., 70100023)")
	flag.StringVar(&location, "l", "", "Location for recipe sourcing (short form)")
	flag.StringVar(&location, "style", "", "filter wine style")
	flag.BoolVar(&verbose, "verbose", false, "dump all ingredients and grades")
	flag.Parse()
	ctx := context.Background()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load configuration: %s", err)
	}

	sp, err := recipes.NewStaplesProvider(cfg)
	if err != nil {
		log.Fatalf("failed to create recipe generator: %s", err)
	}

	wines, err := sp.FetchWines(ctx, location, []string{style})
	if err != nil {
		log.Fatalf("failed to get ingredients: %s", err)
	}

	for _, result := range wines {
		fmt.Printf(
			"%s - %s: regular: %s sale: %s:\n",
			result.Brand,
			result.Description,
			priceString(result.PriceRegular),
			priceString(result.PriceSale),
		)
	}

	fmt.Printf("Total count %d ", len(wines))
}

func priceString(price *float32) string {
	if price == nil {
		return "-"
	}
	return fmt.Sprintf("%.2f", *price)
}
