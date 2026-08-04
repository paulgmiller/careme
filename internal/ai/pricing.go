package ai

import (
	"log/slog"
	"math"
	"strings"
)

const tokensPerMillion = 1_000_000

type textTokenPrice struct {
	inputUSDPerMillion       float64
	cachedInputUSDPerMillion float64
	cacheWriteUSDPerMillion  float64
	outputUSDPerMillion      float64
}

type estimatedSpend struct {
	reason         string
	inputUSD       float64
	cachedInputUSD float64
	cacheWriteUSD  float64
	outputUSD      float64
}

func (s estimatedSpend) totalUSD() float64 {
	return s.inputUSD + s.cachedInputUSD + s.cacheWriteUSD + s.outputUSD
}

func estimatedSpendLogAttr(spend estimatedSpend) slog.Attr {
	if spend.reason != "" {
		return slog.Group("spend",
			slog.String("reason", spend.reason),
		)
	}
	return slog.Group("spend",
		slog.String("currency", "USD"),
		slog.Float64("totalUSD", roundUSD(spend.totalUSD())),
		slog.Float64("inputUSD", roundUSD(spend.inputUSD)),
		slog.Float64("cachedInputUSD", roundUSD(spend.cachedInputUSD)),
		slog.Float64("cacheWriteInputUSD", roundUSD(spend.cacheWriteUSD)),
		slog.Float64("outputUSD", roundUSD(spend.outputUSD)),
	)
}

func estimateOpenAIResponseSpend(model string, inputTokens, cachedInputTokens, cacheWriteTokens, outputTokens int64) estimatedSpend {
	price, ok := openAITextTokenPrice(model)
	if !ok {
		return estimatedSpend{reason: "price_not_configured"}
	}
	if cachedInputTokens > inputTokens {
		cachedInputTokens = inputTokens
	}
	if cachedInputTokens < 0 {
		cachedInputTokens = 0
	}
	if cacheWriteTokens < 0 {
		cacheWriteTokens = 0
	}
	if cacheWriteTokens > inputTokens-cachedInputTokens {
		cacheWriteTokens = inputTokens - cachedInputTokens
	}
	uncachedInputTokens := inputTokens - cachedInputTokens - cacheWriteTokens
	cacheWriteRate := price.cacheWriteUSDPerMillion
	if cacheWriteRate == 0 {
		// Before GPT-5.6, cache writes have no premium and remain ordinary input.
		cacheWriteRate = price.inputUSDPerMillion
	}
	return estimatedSpend{
		inputUSD:       tokensToUSD(uncachedInputTokens, price.inputUSDPerMillion),
		cachedInputUSD: tokensToUSD(cachedInputTokens, price.cachedInputUSDPerMillion),
		cacheWriteUSD:  tokensToUSD(cacheWriteTokens, cacheWriteRate),
		outputUSD:      tokensToUSD(outputTokens, price.outputUSDPerMillion),
	}
}

func openAITextTokenPrice(model string) (textTokenPrice, bool) {
	// Standard paid-tier USD per 1M tokens, verified 2026-07-09:
	// https://openai.com/api/pricing/ and https://platform.openai.com/docs/pricing/
	switch normalizeModelName(model) {
	case "gpt-5.6", "gpt-5.6-sol":
		return textTokenPrice{inputUSDPerMillion: 5, cachedInputUSDPerMillion: 0.50, cacheWriteUSDPerMillion: 6.25, outputUSDPerMillion: 30}, true
	case "gpt-5.6-terra":
		return textTokenPrice{inputUSDPerMillion: 2.50, cachedInputUSDPerMillion: 0.25, cacheWriteUSDPerMillion: 3.125, outputUSDPerMillion: 15}, true
	case "gpt-5.6-luna":
		return textTokenPrice{inputUSDPerMillion: 1, cachedInputUSDPerMillion: 0.10, cacheWriteUSDPerMillion: 1.25, outputUSDPerMillion: 6}, true
	case "gpt-5.5":
		return textTokenPrice{inputUSDPerMillion: 5, cachedInputUSDPerMillion: 0.50, outputUSDPerMillion: 30}, true
	case "gpt-5.4":
		return textTokenPrice{inputUSDPerMillion: 2.50, cachedInputUSDPerMillion: 0.25, outputUSDPerMillion: 15}, true
	case "gpt-5.4-mini":
		return textTokenPrice{inputUSDPerMillion: 0.75, cachedInputUSDPerMillion: 0.075, outputUSDPerMillion: 4.50}, true
	case "gpt-5.2":
		return textTokenPrice{inputUSDPerMillion: 1.75, cachedInputUSDPerMillion: 0.175, outputUSDPerMillion: 14}, true
	case "gpt-5.1", "gpt-5":
		return textTokenPrice{inputUSDPerMillion: 1.25, cachedInputUSDPerMillion: 0.125, outputUSDPerMillion: 10}, true
	case "gpt-5-mini":
		return textTokenPrice{inputUSDPerMillion: 0.25, cachedInputUSDPerMillion: 0.025, outputUSDPerMillion: 2}, true
	case "gpt-5-nano":
		return textTokenPrice{inputUSDPerMillion: 0.05, cachedInputUSDPerMillion: 0.005, outputUSDPerMillion: 0.40}, true
	default:
		return textTokenPrice{}, false
	}
}

func estimateOpenAIImageSpend(model string, textInputTokens, imageInputTokens, outputTokens int64) estimatedSpend {
	switch normalizeModelName(model) {
	case "gpt-image-2":
		return estimatedSpend{
			inputUSD: tokensToUSD(textInputTokens, 5) +
				tokensToUSD(imageInputTokens, 8),
			outputUSD: tokensToUSD(outputTokens, 30),
		}
	default:
		return estimatedSpend{reason: "price_not_configured"}
	}
}

func tokensToUSD(tokens int64, usdPerMillion float64) float64 {
	if tokens <= 0 || usdPerMillion <= 0 {
		return 0
	}
	return float64(tokens) * usdPerMillion / tokensPerMillion
}

func normalizeModelName(model string) string {
	return strings.ToLower(strings.TrimSpace(model))
}

func roundUSD(value float64) float64 {
	return math.Round(value*1_000_000_000) / 1_000_000_000
}
