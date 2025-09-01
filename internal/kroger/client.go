package kroger

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
)

type Client struct {
	apiKey     string
	httpClient *http.Client
	baseURL    string
}

type Product struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Brand       string  `json:"brand"`
	Price       float64 `json:"price"`
	SalePrice   float64 `json:"sale_price"`
	OnSale      bool    `json:"on_sale"`
	Available   bool    `json:"available"`
	Fresh       bool    `json:"fresh"`
	Category    string  `json:"category"`
	Description string  `json:"description"`
}

type SalesRequest struct {
	Location string `json:"location"`
}

type SalesResponse struct {
	Products []Product `json:"products"`
	Total    int       `json:"total"`
}

func NewClient(apiKey string) *Client {
	return &Client{
		apiKey:     apiKey,
		httpClient: &http.Client{},
		baseURL:    "https://api.kroger.com/v1",
	}
}

func (c *Client) GetSaleProducts(location string) ([]Product, error) {
	request := SalesRequest{
		Location: location,
	}

	jsonData, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", c.baseURL+"/products/sales", bytes.NewBuffer(jsonData))
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

	var salesResp SalesResponse
	if err := json.NewDecoder(resp.Body).Decode(&salesResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return salesResp.Products, nil
}