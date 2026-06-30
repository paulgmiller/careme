package main

import (
	"context"

	"careme/internal/campaigns"
	"careme/internal/config"
)

func runAdvertisedRecipeGeneration(ctx context.Context, cfg *config.Config) error {
	return campaigns.RunAdvertisedRecipeGeneration(ctx, cfg)
}
