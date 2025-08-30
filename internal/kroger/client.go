package kroger

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
)

type Client struct {
	mcpServerURL string
	apiKey       string
	httpClient   *http.Client
}

type Product struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Brand       string  `json:"brand"`
	Price       float64 `json:"price"`
	Available   bool    `json:"available"`
	Fresh       bool    `json:"fresh"`
	Category    string  `json:"category"`
	Description string  `json:"description"`
}

type SearchRequest struct {
	Location   string   `json:"location"`
	Keywords   []string `json:"keywords"`
	Categories []string `json:"categories"`
	FreshOnly  bool     `json:"fresh_only"`
}

type SearchResponse struct {
	Products []Product `json:"products"`
	Total    int       `json:"total"`
}

func NewClient(mcpServerURL, apiKey string) *Client {
	return &Client{
		mcpServerURL: mcpServerURL,
		apiKey:       apiKey,
		httpClient:   &http.Client{},
	}
}

func (c *Client) SearchProducts(location string, keywords []string, freshOnly bool) ([]Product, error) {
	request := SearchRequest{
		Location:  location,
		Keywords:  keywords,
		FreshOnly: freshOnly,
	}

	jsonData, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", c.mcpServerURL+"/search", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API request failed with status: %d", resp.StatusCode)
	}

	var searchResp SearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&searchResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return searchResp.Products, nil
}

func (c *Client) GetFreshIngredients(location string, ingredients []string) ([]Product, error) {
	return c.SearchProducts(location, ingredients, true)
}