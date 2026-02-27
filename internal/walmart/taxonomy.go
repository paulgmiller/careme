package walmart

import (
	"encoding/json"
	"fmt"
	"io"
)

// Taxonomy represents Walmart's category taxonomy payload.
type Taxonomy struct {
	Categories []TaxonomyCategory `json:"categories"`
}

// TaxonomyCategory represents a taxonomy node and its optional child nodes.
type TaxonomyCategory struct {
	ID       string             `json:"id"`
	Name     string             `json:"name"`
	Path     string             `json:"path"`
	Children []TaxonomyCategory `json:"children,omitempty"`
}

// ParseTaxonomy unmarshals Walmart taxonomy JSON payloads.
func ParseTaxonomy(r io.Reader) (*Taxonomy, error) {
	var taxonomy Taxonomy

	if err := json.NewDecoder(r).Decode(&taxonomy); err != nil {
		return nil, fmt.Errorf("unmarshal taxonomy: %w", err)
	}
	return &taxonomy, nil
}
