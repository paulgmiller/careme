package safewayads

import "time"

type StoreDetailsResponse struct {
	Store StoreDetails `json:"store"`
}

type StoreDetails struct {
	LocationID string       `json:"locationId"`
	DomainName string       `json:"domainName"`
	Address    StoreAddress `json:"address"`
}

type StoreAddress struct {
	ZipCode string `json:"zipcode"`
	State   string `json:"state"`
	City    string `json:"city"`
	Line1   string `json:"line1"`
}

type Publication struct {
	ID                        int64  `json:"id"`
	FlyerRunID                int64  `json:"flyer_run_id"`
	Name                      string `json:"name"`
	ExternalDisplayName       string `json:"external_display_name"`
	DeepLink                  string `json:"deep_link"`
	StorefrontPayloadURL      string `json:"storefront_payload_url"`
	TotalPages                int    `json:"total_pages"`
	PostalCode                string `json:"postal_code"`
	ThumbnailImageURL         string `json:"thumbnail_image_url"`
	FirstPageThumbnailURL     string `json:"first_page_thumbnail_url"`
	FirstPageThumbnail2000URL string `json:"first_page_thumbnail_2000h_url"`
	ImageFirstPage400W        string `json:"image_first_page_400w"`
	PDFURL                    string `json:"pdf_url"`
	ValidFrom                 string `json:"valid_from"`
	ValidTo                   string `json:"valid_to"`
}

type PageAsset struct {
	PageNumber       int    `json:"page_number"`
	ImageURL         string `json:"image_url,omitempty"`
	ImageKey         string `json:"image_key,omitempty"`
	ImageChecksum    string `json:"image_checksum,omitempty"`
	ImageContentType string `json:"image_content_type,omitempty"`
}

type RunStatus struct {
	StoreID          string      `json:"store_id"`
	StoreCode        string      `json:"store_code"`
	Status           string      `json:"status"`
	UpdatedAt        time.Time   `json:"updated_at"`
	StoreName        string      `json:"store_name,omitempty"`
	City             string      `json:"city,omitempty"`
	State            string      `json:"state,omitempty"`
	PostalCode       string      `json:"postal_code,omitempty"`
	SourcePageURL    string      `json:"source_page_url,omitempty"`
	PublicationID    int64       `json:"publication_id,omitempty"`
	PublicationName  string      `json:"publication_name,omitempty"`
	PDFURL           string      `json:"pdf_url,omitempty"`
	PageCount        int         `json:"page_count,omitempty"`
	Pages            []PageAsset `json:"pages,omitempty"`
	ImageURL         string      `json:"image_url,omitempty"`
	ImageKey         string      `json:"image_key,omitempty"`
	ImageChecksum    string      `json:"image_checksum,omitempty"`
	ImageContentType string      `json:"image_content_type,omitempty"`
	IngredientsKey   string      `json:"ingredients_key,omitempty"`
	IngredientCount  int         `json:"ingredient_count,omitempty"`
	Error            string      `json:"error,omitempty"`
}

type IngredientDocument[T any] struct {
	StoreID         string      `json:"store_id"`
	StoreCode       string      `json:"store_code"`
	PublicationID   int64       `json:"publication_id"`
	PublicationName string      `json:"publication_name"`
	ExtractedAt     time.Time   `json:"extracted_at"`
	PDFURL          string      `json:"pdf_url,omitempty"`
	PageCount       int         `json:"page_count,omitempty"`
	Pages           []PageAsset `json:"pages,omitempty"`
	ImageURL        string      `json:"image_url"`
	ImageKey        string      `json:"image_key"`
	ImageChecksum   string      `json:"image_checksum"`
	Ingredients     []T         `json:"ingredients"`
}
