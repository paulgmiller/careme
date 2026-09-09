package ai

import (
	"context"
	"time"

	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
)

func (c *client) newRecipeResponse(ctx context.Context, params responses.ResponseNewParams) (*responses.Response, error) {
	params.ServiceTier = c.serviceTier
	if params.ServiceTier == responses.ResponseNewParamsServiceTierFlex {
		return c.oai.Responses.New(ctx, params, option.WithRequestTimeout(15*time.Minute))
	}
	return c.oai.Responses.New(ctx, params)
}
