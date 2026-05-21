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
	outputUSDPerMillion      float64
}

type estimatedSpend struct {
	available      bool
	pricingMode    string
	reason         string
	inputUSD       float64
	cachedInputUSD float64
	outputUSD      float64
}

func (s estimatedSpend) totalUSD() float64 {
	return s.inputUSD + s.cachedInputUSD + s.outputUSD
}

func estimatedSpendLogAttr(spend estimatedSpend) slog.Attr {
	if !spend.available {
		return slog.Group("spend",
			slog.Bool("available", false),
			slog.String("reason", spend.reason),
		)
	}
	return slog.Group("spend",
		slog.Bool("available", true),
		slog.String("currency", "USD"),
		slog.String("pricingMode", spend.pricingMode),
		slog.Float64("estimatedUSD", roundUSD(spend.totalUSD())),
		slog.Float64("inputUSD", roundUSD(spend.inputUSD)),
		slog.Float64("cachedInputUSD", roundUSD(spend.cachedInputUSD)),
		slog.Float64("outputUSD", roundUSD(spend.outputUSD)),
	)
}

func estimateOpenAIResponseSpend(model string, inputTokens, cachedInputTokens, outputTokens int64) estimatedSpend {
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
	uncachedInputTokens := inputTokens - cachedInputTokens
	return estimatedSpend{
		available:      true,
		pricingMode:    "standard",
		inputUSD:       tokensToUSD(uncachedInputTokens, price.inputUSDPerMillion),
		cachedInputUSD: tokensToUSD(cachedInputTokens, price.cachedInputUSDPerMillion),
		outputUSD:      tokensToUSD(outputTokens, price.outputUSDPerMillion),
	}
}

func openAITextTokenPrice(model string) (textTokenPrice, bool) {
	// Standard paid-tier USD per 1M tokens, verified 2026-05-21:
	// https://openai.com/api/pricing/ and https://platform.openai.com/docs/pricing/
	switch normalizeModelName(model) {
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
			available:   true,
			pricingMode: "standard",
			inputUSD: tokensToUSD(textInputTokens, 5) +
				tokensToUSD(imageInputTokens, 8),
			outputUSD: tokensToUSD(outputTokens, 30),
		}
	default:
		return estimatedSpend{reason: "price_not_configured"}
	}
}

func estimateGeminiSpend(model string, promptTokens, cachedTokens, outputTokens int64) estimatedSpend {
	price, ok := geminiTextTokenPrice(model, promptTokens)
	if !ok {
		return estimatedSpend{reason: "price_not_configured"}
	}
	if cachedTokens > promptTokens {
		cachedTokens = promptTokens
	}
	if cachedTokens < 0 {
		cachedTokens = 0
	}
	uncachedPromptTokens := promptTokens - cachedTokens
	return estimatedSpend{
		available:      true,
		pricingMode:    "standard",
		inputUSD:       tokensToUSD(uncachedPromptTokens, price.inputUSDPerMillion),
		cachedInputUSD: tokensToUSD(cachedTokens, price.cachedInputUSDPerMillion),
		outputUSD:      tokensToUSD(outputTokens, price.outputUSDPerMillion),
	}
}

func geminiTextTokenPrice(model string, promptTokens int64) (textTokenPrice, bool) {
	// Standard paid-tier USD per 1M tokens, verified 2026-05-21:
	// https://ai.google.dev/gemini-api/docs/pricing
	largePrompt := promptTokens > 200_000
	switch normalizeModelName(model) {
	case "gemini-3.5-flash":
		return textTokenPrice{inputUSDPerMillion: 1.50, cachedInputUSDPerMillion: 0.15, outputUSDPerMillion: 9}, true
	case "gemini-3.1-pro-preview", "gemini-3.1-pro-preview-customtools":
		if largePrompt {
			return textTokenPrice{inputUSDPerMillion: 4, cachedInputUSDPerMillion: 0.40, outputUSDPerMillion: 18}, true
		}
		return textTokenPrice{inputUSDPerMillion: 2, cachedInputUSDPerMillion: 0.20, outputUSDPerMillion: 12}, true
	case "gemini-3.1-flash-lite", "gemini-3.1-flash-lite-preview":
		return textTokenPrice{inputUSDPerMillion: 0.25, cachedInputUSDPerMillion: 0.025, outputUSDPerMillion: 1.50}, true
	case "gemini-3-flash-preview":
		return textTokenPrice{inputUSDPerMillion: 0.50, cachedInputUSDPerMillion: 0.05, outputUSDPerMillion: 3}, true
	case "gemini-2.5-pro":
		if largePrompt {
			return textTokenPrice{inputUSDPerMillion: 2.50, cachedInputUSDPerMillion: 0.25, outputUSDPerMillion: 15}, true
		}
		return textTokenPrice{inputUSDPerMillion: 1.25, cachedInputUSDPerMillion: 0.125, outputUSDPerMillion: 10}, true
	case "gemini-2.5-flash":
		return textTokenPrice{inputUSDPerMillion: 0.30, cachedInputUSDPerMillion: 0.03, outputUSDPerMillion: 2.50}, true
	case "gemini-2.5-flash-lite", "gemini-2.5-flash-lite-preview-09-2025":
		return textTokenPrice{inputUSDPerMillion: 0.10, cachedInputUSDPerMillion: 0.01, outputUSDPerMillion: 0.40}, true
	default:
		return textTokenPrice{}, false
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
