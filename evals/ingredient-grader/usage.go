package eval

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
)

// Capture only billing metadata; never retain request headers or generated output.
type usageTransport struct {
	base    http.RoundTripper
	mu      sync.Mutex
	records []usageRecord
	err     error
}

type usageRecord struct {
	Endpoint         string  `json:"endpoint"`
	Model            string  `json:"model"`
	ServiceTier      string  `json:"serviceTier,omitempty"`
	InputTokens      int64   `json:"inputTokens"`
	CachedTokens     int64   `json:"cachedTokens"`
	CacheWriteTokens int64   `json:"cacheWriteTokens"`
	OutputTokens     int64   `json:"outputTokens"`
	CostUSD          float64 `json:"estimatedCostUSD"`
}

func (u *usageTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := u.base.RoundTrip(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		return resp, err
	}
	body, err := io.ReadAll(resp.Body)
	closeErr := resp.Body.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	resp.Body = io.NopCloser(bytes.NewReader(body))
	var response struct {
		Model       string `json:"model"`
		ServiceTier string `json:"service_tier"`
		Usage       *struct {
			InputTokens  int64 `json:"input_tokens"`
			OutputTokens int64 `json:"output_tokens"`
			Details      struct {
				CachedTokens     int64 `json:"cached_tokens"`
				CacheWriteTokens int64 `json:"cache_write_tokens"`
			} `json:"input_tokens_details"`
		} `json:"usage"`
	}
	err = json.Unmarshal(body, &response)
	record := usageRecord{Endpoint: req.URL.Path, Model: response.Model, ServiceTier: response.ServiceTier}
	if err == nil {
		if response.Usage == nil {
			err = fmt.Errorf("API response omitted token usage")
		} else {
			record.InputTokens = response.Usage.InputTokens
			record.OutputTokens = response.Usage.OutputTokens
			record.CachedTokens = response.Usage.Details.CachedTokens
			record.CacheWriteTokens = response.Usage.Details.CacheWriteTokens
			record.CostUSD, err = ingredientEvalCost(record)
		}
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	if err != nil {
		u.err = err
	} else {
		u.records = append(u.records, record)
	}
	return resp, nil
}

func (u *usageTransport) snapshot() ([]usageRecord, float64, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.err != nil {
		return nil, 0, u.err
	}
	if len(u.records) == 0 {
		return nil, 0, fmt.Errorf("no API token usage recorded")
	}
	var total float64
	for _, r := range u.records {
		total += r.CostUSD
	}
	return append([]usageRecord(nil), u.records...), total, nil
}

func ingredientEvalCost(r usageRecord) (float64, error) {
	// Standard short-context rates verified 2026-10-08 against official model
	// and Decisions docs. Estimates exclude taxes and account-specific discounts.
	if r.InputTokens <= 0 || r.InputTokens > 272000 {
		return 0, fmt.Errorf("invalid or long-context input usage for cost estimate")
	}
	if r.Endpoint == "/v1/decisions" && r.Model == "gpt-6-luna" {
		return float64(r.InputTokens) * 0.10 / 1e6, nil
	}
	if r.Endpoint != "/v1/responses" {
		return 0, fmt.Errorf("unsupported cost endpoint %q", r.Endpoint)
	}
	var input, cached, write, output float64
	switch r.Model {
	case "gpt-6-luna":
		input, cached, write, output = 0.10, 0.01, 0.125, 0.50
	case "gpt-5.6-luna":
		input, cached, write, output = 0.20, 0.02, 0.25, 1.20
	default:
		return 0, fmt.Errorf("ingredient eval price not configured for %q", r.Model)
	}
	if r.CachedTokens < 0 || r.CacheWriteTokens < 0 || r.OutputTokens < 0 || r.CachedTokens+r.CacheWriteTokens > r.InputTokens {
		return 0, fmt.Errorf("invalid token usage for cost estimate")
	}
	cost := (float64(r.InputTokens-r.CachedTokens-r.CacheWriteTokens)*input + float64(r.CachedTokens)*cached + float64(r.CacheWriteTokens)*write + float64(r.OutputTokens)*output) / 1e6
	switch r.ServiceTier {
	case "", "default", "standard":
	case "flex", "batch":
		cost /= 2
	case "fast", "priority":
		cost *= 2
	default:
		return 0, fmt.Errorf("unsupported service tier %q", r.ServiceTier)
	}
	return cost, nil
}
