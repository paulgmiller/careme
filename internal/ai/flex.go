package ai

import (
	"context"
	"time"

	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
)

type flexProcessingKey struct{}

// WithFlexProcessing opts background recipe and menu requests into flex pricing.
// Images use a separate endpoint that does not support service_tier.
func WithFlexProcessing(ctx context.Context) context.Context {
	return context.WithValue(ctx, flexProcessingKey{}, true)
}

// RecipeServiceTier returns the processing tier for recipe and menu requests.
func RecipeServiceTier(ctx context.Context) responses.ResponseNewParamsServiceTier {
	if enabled, _ := ctx.Value(flexProcessingKey{}).(bool); enabled {
		return responses.ResponseNewParamsServiceTierFlex
	}
	return ""
}

func (c *client) newRecipeResponse(ctx context.Context, params responses.ResponseNewParams) (*responses.Response, error) {
	params.ServiceTier = RecipeServiceTier(ctx)
	if params.ServiceTier == responses.ResponseNewParamsServiceTierFlex {
		return c.oai.Responses.New(ctx, params, option.WithRequestTimeout(15*time.Minute))
	}
	return c.oai.Responses.New(ctx, params)
}
