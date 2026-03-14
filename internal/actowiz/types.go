package actowiz

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

type SafewayProduct struct {
	StoreName          string   `json:"Store Name"`
	ZipCode            string   `json:"Zip-Code"`
	ProductName        string   `json:"Product Name"`
	ID                 int64    `json:"ID"`
	URL                string   `json:"URL"`
	ProductDescription string   `json:"Product Description"` // this is really long
	MRP                *float64 `json:"MRP"`
	DiscountedPrice    *float64 `json:"Discounted Price"`
	Category           string   `json:"Category"`
	SubCategory        string   `json:"Sub-Category"`
	Availability       bool     `json:"Availability"`
}

// custom marshalling mostly to handle fact that prices get "N/A" sometimes
func (p *SafewayProduct) UnmarshalJSON(data []byte) error {
	type rawSafewayProduct struct {
		StoreName          string          `json:"Store Name"`
		ZipCode            string          `json:"Zip-Code"`
		ProductName        string          `json:"Product Name"`
		ID                 int64           `json:"ID"`
		URL                string          `json:"URL"`
		ProductDescription string          `json:"Product Description"`
		MRP                json.RawMessage `json:"MRP"`
		DiscountedPrice    json.RawMessage `json:"Discounted Price"`
		Category           string          `json:"Category"`
		SubCategory        string          `json:"Sub-Category"`
		Availability       bool            `json:"Availability"`
	}

	var raw rawSafewayProduct
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	*p = SafewayProduct{
		StoreName:          raw.StoreName,
		ZipCode:            raw.ZipCode,
		ProductName:        raw.ProductName,
		ID:                 raw.ID,
		URL:                raw.URL,
		ProductDescription: raw.ProductDescription,
		Category:           raw.Category,
		SubCategory:        raw.SubCategory,
		Availability:       raw.Availability,
	}

	var err error
	if p.MRP, err = float64Ptr(raw.MRP); err != nil {
		return fmt.Errorf("decode MRP: %w", err)
	}
	if p.DiscountedPrice, err = float64Ptr(raw.DiscountedPrice); err != nil {
		return fmt.Errorf("decode Discounted Price: %w", err)
	}
	return nil
}

func float64Ptr(data json.RawMessage) (*float64, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}

	var number *float64
	if err := json.Unmarshal(trimmed, &number); err == nil {
		return number, nil
	}

	var raw string
	if err := json.Unmarshal(trimmed, &raw); err != nil {
		return nil, err
	}
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.EqualFold(raw, "N/A") {
		return nil, nil
	}
	return nil, fmt.Errorf("unsupported numeric value %q", raw)
}
