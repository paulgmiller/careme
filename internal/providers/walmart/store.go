package walmart

import (
	"encoding/json"
	"fmt"
)

// Store represents a Walmart store location returned by the stores API.
type storeRecord struct {
	No            int         `json:"no"`
	Name          string      `json:"name"`
	Country       string      `json:"country"`
	Coordinates   coordinates `json:"coordinates"`
	StreetAddress string      `json:"streetAddress"`
	City          string      `json:"city"`
	StateProvCode string      `json:"stateProvCode"`
	Zip           string      `json:"zip"`
	PhoneNumber   string      `json:"phoneNumber"`
	SundayOpen    bool        `json:"sundayOpen"`
	Timezone      string      `json:"timezone"`
}

// coordinates stores Walmart's [longitude, latitude] coordinate tuple.
type coordinates struct {
	Longitude float64
	Latitude  float64
}

func (c *coordinates) UnmarshalJSON(data []byte) error {
	var tuple []float64
	if err := json.Unmarshal(data, &tuple); err != nil {
		return fmt.Errorf("unmarshal coordinates: %w", err)
	}
	if len(tuple) != 2 {
		return fmt.Errorf("coordinates must contain [longitude, latitude], got %d values", len(tuple))
	}

	c.Longitude = tuple[0]
	c.Latitude = tuple[1]
	return nil
}

func (c coordinates) MarshalJSON() ([]byte, error) {
	return json.Marshal([]float64{c.Longitude, c.Latitude})
}

// parseStore unmarshals a single store JSON object.
func parseStore(data []byte) (storeRecord, error) {
	var store storeRecord
	if err := json.Unmarshal(data, &store); err != nil {
		return storeRecord{}, fmt.Errorf("unmarshal store: %w", err)
	}
	return store, nil
}

// parseStores unmarshals store payloads from array, wrapped, or single-object shapes.
func parseStores(data []byte) ([]storeRecord, error) {
	var stores []storeRecord
	if err := json.Unmarshal(data, &stores); err == nil {
		return stores, nil
	}

	var wrapped struct {
		Results json.RawMessage `json:"results"`
		Stores  json.RawMessage `json:"stores"`
	}
	if err := json.Unmarshal(data, &wrapped); err == nil {
		if len(wrapped.Results) > 0 {
			var results []storeRecord
			if err := json.Unmarshal(wrapped.Results, &results); err != nil {
				return nil, fmt.Errorf("unmarshal results stores: %w", err)
			}
			return results, nil
		}
		if len(wrapped.Stores) > 0 {
			var nestedStores []storeRecord
			if err := json.Unmarshal(wrapped.Stores, &nestedStores); err != nil {
				return nil, fmt.Errorf("unmarshal stores field: %w", err)
			}
			return nestedStores, nil
		}
	}

	store, err := parseStore(data)
	if err != nil {
		return nil, fmt.Errorf("unmarshal stores payload: %w", err)
	}
	return []storeRecord{store}, nil
}
