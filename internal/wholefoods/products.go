package wholefoods

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
)

const (
	defaultProgramType       = "GROCERY"
	defaultProductSearchSize = 30
	defaultProductSearchSort = "relevanceblender"
)

// ProductSearchRequest configures a call to the Whole Foods product search API.
type ProductSearchRequest struct {
	Text                      string
	OfferListingDiscriminator string
	Offset                    int
	Size                      int
	Sort                      string
	ProgramType               string
	Filters                   string
	Categories                []string
}

// ProductSearchResponse matches the public Whole Foods search payload returned by the WWOS RSI API.
type ProductSearchResponse struct {
	MainResultSet ProductSearchResultSet `json:"mainResultSet"`
}

type ProductSearchResultSet struct {
	SearchResults               []ProductSearchResult              `json:"searchResults"`
	ApproximateTotalResultCount int                                `json:"approximateTotalResultCount"`
	AvailableTotalResultCount   int                                `json:"availableTotalResultCount"`
	TotalResultCountPreVE       int                                `json:"totalResultCountPreVE"`
	Keywords                    string                             `json:"keywords"`
	AugmentModifications        []ProductSearchAugmentModification `json:"augmentModifications,omitempty"`
}

type ProductSearchResult struct {
	ASIN                    string `json:"asin"`
	InjectionSource         string `json:"injectionSource"`
	IsAdultProduct          bool   `json:"isAdultProduct"`
	ProductGroup            string `json:"productGroup,omitempty"`
	AmazonsChoiceExactLabel bool   `json:"amazonsChoiceExactLabel"`
}

type ProductSearchAugmentModification struct {
	Action   string            `json:"action"`
	Type     string            `json:"type"`
	Source   string            `json:"source"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// ProductHydrationRequest configures a call to the Whole Foods product hydration API.
type ProductHydrationRequest struct {
	OfferListingDiscriminator string
	ProgramType               string
	ASINs                     []string
}

// ProductHydrationResponse matches the public Whole Foods WWOS product hydration payload.
type ProductHydrationResponse []HydratedProduct

type HydratedProduct struct {
	BrandName             string                         `json:"brandName"`
	Name                  string                         `json:"name"`
	ASIN                  string                         `json:"asin"`
	ProgramType           string                         `json:"programType"`
	Description           string                         `json:"description"`
	About                 []string                       `json:"about"`
	ProductImages         []string                       `json:"productImages"`
	Availability          string                         `json:"availability"`
	PDPType               string                         `json:"pdpType"`
	OfferDetails          *HydratedOfferDetails          `json:"offerDetails"`
	VariableUnitOfMeasure *HydratedVariableUnitOfMeasure `json:"variableUnitOfMeasure"`
	CTATag                string                         `json:"ctaTag,omitempty"`
	DeliveryPromiseHTML   string                         `json:"deliveryPromiseHtml,omitempty"`
	DietTypes             []string                       `json:"dietTypes,omitempty"`
	Category              HydratedCategory               `json:"category"`
}

type HydratedOfferDetails struct {
	Price                   HydratedPrice `json:"price"`
	OfferListingID          string        `json:"offerListingId"`
	MaxOrderQuantity        int           `json:"maxOrderQuantity"`
	IsMaxQuantityRestricted bool          `json:"isMaxQuantityRestricted"`
}

type HydratedPrice struct {
	CurrencyCode     string               `json:"currencyCode"`
	PriceAmount      float64              `json:"priceAmount"`
	BasisPriceAmount *float64             `json:"basisPriceAmount"`
	Savings          HydratedSavings      `json:"savings"`
	PrimeBenefit     HydratedPrimeBenefit `json:"primeBenefit"`
}

type HydratedSavings struct {
	CurrencyCode   *string  `json:"currencyCode"`
	SavingsAmount  *float64 `json:"savingsAmount"`
	PercentSavings *float64 `json:"percentSavings"`
}

type HydratedPrimeBenefit struct {
	IsApplied     *bool    `json:"isApplied"`
	Text          *string  `json:"text"`
	CurrencyCode  *string  `json:"currencyCode"`
	PriceAmount   *float64 `json:"priceAmount"`
	SavingsAmount *float64 `json:"savingsAmount"`
}

type HydratedVariableUnitOfMeasure struct {
	PricingUOM       HydratedUnitOfMeasure  `json:"pricingUom"`
	SellingUOM       HydratedUnitOfMeasure  `json:"sellingUom"`
	SelectorItemList []HydratedSelectorItem `json:"selectorItemList"`
}

type HydratedUnitOfMeasure struct {
	Dimension string `json:"dimension"`
	Unit      string `json:"unit"`
}

type HydratedSelectorItem struct {
	SelectorPrice                 HydratedSelectorPrice `json:"selectorPrice"`
	SelectorSellingQuantityString string                `json:"selectorSellingQuantityString"`
	SelectorSellingQuantityValue  int                   `json:"selectorSellingQuantityValue"`
}

type HydratedSelectorPrice struct {
	BaseUnit     *string `json:"baseUnit"`
	CurrencyCode string  `json:"currencyCode"`
	PriceAmount  float64 `json:"priceAmount"`
}

type HydratedCategory struct {
	ProductType          string `json:"productType"`
	GLProductGroupSymbol string `json:"glProductGroupSymbol"`
	DisplayName          string `json:"displayName"`
}

// ProductSearch fetches search results from
// https://www.wholefoodsmarket.com/api/wwos/rsi/search?text=merlot&old=A04C&offset=0&size=30&sort=relevanceblender&programType=GROCERY&filters=&categories=18473610011
// where the Whole Foods API uses the query parameter name "old" for the offer listing discriminator.
func (c *Client) ProductSearch(ctx context.Context, req ProductSearchRequest) (*ProductSearchResponse, error) {
	text := strings.TrimSpace(req.Text)
	if text == "" {
		return nil, errors.New("text is required")
	}

	discriminator := strings.TrimSpace(req.OfferListingDiscriminator)
	if discriminator == "" {
		return nil, errors.New("offer listing discriminator is required")
	}

	if req.Offset < 0 {
		return nil, errors.New("offset must be >= 0")
	}
	if req.Size < 0 {
		return nil, errors.New("size must be >= 0")
	}

	size := req.Size
	if size == 0 {
		size = defaultProductSearchSize
	}

	sort := strings.TrimSpace(req.Sort)
	if sort == "" {
		sort = defaultProductSearchSort
	}

	programType := strings.TrimSpace(req.ProgramType)
	if programType == "" {
		programType = defaultProgramType
	}

	endpoint, err := url.Parse(c.baseURL + "/api/wwos/rsi/search")
	if err != nil {
		return nil, fmt.Errorf("parse product search URL: %w", err)
	}

	params := endpoint.Query()
	params.Set("text", text)
	params.Set("old", discriminator)
	params.Set("offset", strconv.Itoa(req.Offset))
	params.Set("size", strconv.Itoa(size))
	params.Set("sort", sort)
	params.Set("programType", programType)
	params.Set("filters", req.Filters)
	if categories := joinNonEmpty(req.Categories); categories != "" {
		params.Set("categories", categories)
	}
	endpoint.RawQuery = params.Encode()

	slog.InfoContext(ctx, "wf product search", "url", endpoint)
	var decoded ProductSearchResponse
	if err := c.getJSON(ctx, endpoint.String(), &decoded); err != nil {
		return nil, err
	}
	return &decoded, nil
}

// ProductHydration fetches hydrated product records from
// https://www.wholefoodsmarket.com/api/wwos/products?offerListingDiscriminator=A04C&programType=GROCERY&asins=B06WVGV73Z%2CB07G4TKBFP
// where the asins query parameter is a comma-separated list of Whole Foods ASINs.
func (c *Client) ProductHydration(ctx context.Context, req ProductHydrationRequest) (ProductHydrationResponse, error) {
	discriminator := strings.TrimSpace(req.OfferListingDiscriminator)
	if discriminator == "" {
		return nil, errors.New("offer listing discriminator is required")
	}

	asins := joinNonEmpty(req.ASINs)
	if asins == "" {
		return nil, errors.New("at least one ASIN is required")
	}

	programType := strings.TrimSpace(req.ProgramType)
	if programType == "" {
		programType = defaultProgramType
	}

	endpoint, err := url.Parse(c.baseURL + "/api/wwos/products")
	if err != nil {
		return nil, fmt.Errorf("parse product hydration URL: %w", err)
	}

	params := endpoint.Query()
	params.Set("offerListingDiscriminator", discriminator)
	params.Set("programType", programType)
	params.Set("asins", asins)
	endpoint.RawQuery = params.Encode()

	slog.InfoContext(ctx, "wf product hydration", "url", endpoint)
	var decoded ProductHydrationResponse
	if err := c.getJSON(ctx, endpoint.String(), &decoded); err != nil {
		return nil, err
	}
	return decoded, nil
}

func joinNonEmpty(parts []string) string {
	filtered := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		filtered = append(filtered, part)
	}
	return strings.Join(filtered, ",")
}
