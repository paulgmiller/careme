package actowiz

import (
	"bytes"
	"encoding/json"
	"fmt"
)

type AlbertsonsProduct struct {
	SerialNumber    int     `json:"Sr No"`
	ProductURL      string  `json:"Product URL"`
	ProductName     string  `json:"Product Name"`
	Breadcrumbs     string  `json:"Breadcrumbs"`
	Category        string  `json:"Category"`
	SubCategory     string  `json:"Sub Category"`
	Price           float64 `json:"Price"`
	DiscountedPrice string  `json:"Discounted Price"`
	ImageURL        string  `json:"Image URL"`
	StoreLocation   string  `json:"Store Location"`
	Availability    string  `json:"Availability"`
}

type WholeFoodsProduct struct {
	URL                  string            `json:"url"`
	ProductID            int64             `json:"product_id"`
	ProductName          string            `json:"product_name"`
	ProductJSONName      string            `json:"product_json_name"`
	Brand                string            `json:"brand"`
	Images               StringSlice       `json:"images"`
	IngredientsString    string            `json:"ingredients_string"`
	NutritionalData      NutritionalValues `json:"nutritional_data"`
	StoreID              int64             `json:"store_id"`
	RegularPrice         float64           `json:"regular_price"`
	SalePrice            string            `json:"sale_price"`
	IncrementalSalePrice string            `json:"incremental_sale_price"`
	Allergens            string            `json:"allergens"`
	NetWeight            string            `json:"net_weight"`
	Location             string            `json:"location"`
	PackSize             string            `json:"pack_size"`
	Description          string            `json:"description"`
	Calories             int               `json:"calories"`
}

type StringSlice []string

func (s *StringSlice) UnmarshalJSON(data []byte) error {
	if len(bytes.TrimSpace(data)) == 0 || bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		*s = nil
		return nil
	}

	// Actowiz exports sometimes encode arrays as a JSON string.
	if len(data) > 0 && data[0] == '"' {
		var encoded string
		if err := json.Unmarshal(data, &encoded); err != nil {
			return err
		}
		if encoded == "" {
			*s = nil
			return nil
		}
		return json.Unmarshal([]byte(encoded), (*[]string)(s))
	}

	return json.Unmarshal(data, (*[]string)(s))
}

type NutrientValue struct {
	Value          string  `json:"value"`
	Unit           string  `json:"unit"`
	PerServing     string  `json:"per_serving"`
	FullDailyValue float64 `json:"full_daily_value"`
}

type NutritionalValues map[string]NutrientValue

func (n *NutritionalValues) UnmarshalJSON(data []byte) error {
	if len(bytes.TrimSpace(data)) == 0 || bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		*n = nil
		return nil
	}

	if len(data) > 0 && data[0] == '"' {
		var encoded string
		if err := json.Unmarshal(data, &encoded); err != nil {
			return err
		}
		if encoded == "" {
			*n = nil
			return nil
		}

		tmp := map[string]NutrientValue{}
		if err := json.Unmarshal([]byte(encoded), &tmp); err != nil {
			return fmt.Errorf("decode string-encoded nutritional_data: %w", err)
		}
		*n = tmp
		return nil
	}

	tmp := map[string]NutrientValue{}
	if err := json.Unmarshal(data, &tmp); err != nil {
		return err
	}
	*n = tmp
	return nil
}
