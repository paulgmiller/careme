package ingredients

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type SeasonalClient struct {
	apiEndpoint string
	apiKey      string
	httpClient  *http.Client
}

type SeasonalIngredient struct {
	Name        string   `json:"name"`
	Season      []string `json:"season"`
	Peak        []string `json:"peak"`
	Category    string   `json:"category"`
	Description string   `json:"description"`
	Substitutes []string `json:"substitutes"`
}

type SeasonalResponse struct {
	Ingredients []SeasonalIngredient `json:"ingredients"`
	Season      string               `json:"current_season"`
	Region      string               `json:"region"`
}

func NewSeasonalClient(apiEndpoint, apiKey string) *SeasonalClient {
	return &SeasonalClient{
		apiEndpoint: apiEndpoint,
		apiKey:      apiKey,
		httpClient:  &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *SeasonalClient) GetSeasonalIngredients(location string) ([]SeasonalIngredient, error) {
	season := getCurrentSeason()
	url := fmt.Sprintf("%s/seasonal?location=%s&season=%s", c.apiEndpoint, location, season)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API request failed with status: %d", resp.StatusCode)
	}

	var seasonalResp SeasonalResponse
	if err := json.NewDecoder(resp.Body).Decode(&seasonalResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return seasonalResp.Ingredients, nil
}

func (c *SeasonalClient) GetIngredientsForSeason(season, location string) ([]SeasonalIngredient, error) {
	url := fmt.Sprintf("%s/seasonal?location=%s&season=%s", c.apiEndpoint, location, season)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API request failed with status: %d", resp.StatusCode)
	}

	var seasonalResp SeasonalResponse
	if err := json.NewDecoder(resp.Body).Decode(&seasonalResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return seasonalResp.Ingredients, nil
}

func getCurrentSeason() string {
	now := time.Now()
	month := now.Month()

	switch {
	case month >= time.March && month <= time.May:
		return "spring"
	case month >= time.June && month <= time.August:
		return "summer"
	case month >= time.September && month <= time.November:
		return "fall"
	default:
		return "winter"
	}
}

func (s *SeasonalIngredient) IsInSeason(season string) bool {
	season = strings.ToLower(season)
	for _, s := range s.Season {
		if strings.ToLower(s) == season {
			return true
		}
	}
	return false
}

func (s *SeasonalIngredient) IsAtPeak(season string) bool {
	season = strings.ToLower(season)
	for _, p := range s.Peak {
		if strings.ToLower(p) == season {
			return true
		}
	}
	return false
}