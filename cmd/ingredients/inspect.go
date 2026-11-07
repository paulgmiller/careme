package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"careme/internal/ai"
	"careme/internal/config"
	"careme/internal/providers/kroger"
	"careme/internal/providers/kroger/products"
)

type krogerInspectionProvider interface {
	SearchProducts(context.Context, string, string) ([]products.ProductsProductModel, error)
	FetchStaples(context.Context, string) ([]ai.InputIngredient, error)
}

func inspectKroger(ctx context.Context, location string, terms []string) error {
	cfg, err := config.LoadOnlyProviders()
	if err != nil {
		return fmt.Errorf("load provider configuration: %w", err)
	}
	if cfg.Kroger.ClientID == "" || cfg.Kroger.ClientSecret == "" {
		return fmt.Errorf("kroger inspection requires KROGER_CLIENT_ID and KROGER_CLIENT_SECRET")
	}
	provider, err := kroger.NewStaplesProvider(cfg, http.DefaultClient)
	if err != nil {
		return err
	}
	if !provider.IsID(location) {
		return fmt.Errorf("kroger inspection requires a numeric Kroger location ID")
	}
	report, err := inspectStaples(ctx, provider, location, terms)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(report)
}

func inspectStaples(ctx context.Context, provider krogerInspectionProvider, locationID string, terms []string) (staplesInspection, error) {
	report := staplesInspection{LocationID: locationID}
	for _, term := range terms {
		matches, err := provider.SearchProducts(ctx, locationID, term)
		if err != nil {
			return staplesInspection{}, err
		}
		report.Searches = append(report.Searches, productInspection{Term: term, Products: matches})
	}
	rows, err := provider.FetchStaples(ctx, locationID)
	if err != nil {
		return staplesInspection{}, fmt.Errorf("fetch staples for comparison: %w", err)
	}
	// Only include catalog rows for products returned by the requested searches.
	ids := map[string]bool{}
	for _, search := range report.Searches {
		for _, product := range search.Products {
			if product.ProductId != nil {
				ids[*product.ProductId] = true
			}
		}
	}
	for _, row := range rows {
		if ids[row.ProductID] {
			report.Staples = append(report.Staples, row)
		}
	}
	return report, nil
}

type staplesInspection struct {
	LocationID string               `json:"locationId"`
	Searches   []productInspection  `json:"searches"`
	Staples    []ai.InputIngredient `json:"matchingStaplesBeforeDeduplication"`
}

type productInspection struct {
	Term     string                          `json:"term"`
	Products []products.ProductsProductModel `json:"products"`
}
