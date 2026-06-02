package query

// CollectionProductsPayload matches the useful parts of ALDI's collection
// products GraphQL response.
type CollectionProductsPayload struct {
	Data   CollectionProductsData `json:"data"`
	Errors []GraphQLError         `json:"errors"`
}

type CollectionProductsData struct {
	CollectionProducts                   CollectionProducts                   `json:"collectionProducts"`
	CollectionProductsBasedSearchResults CollectionProductsBasedSearchResults `json:"collectionProductsBasedSearchResults"`
}

type CollectionProducts struct {
	ItemIDs []string `json:"itemIds"`
	Items   []Item   `json:"items"`
}

type CollectionProductsBasedSearchResults struct {
	ItemResultList SearchItemResultList `json:"itemResultList"`
	SearchID       string               `json:"searchId"`
	ViewSection    CollectionView       `json:"viewSection"`
}

type SearchItemResultList struct {
	FeaturedProducts []Item   `json:"featuredProducts"`
	ItemIDs          []string `json:"itemIds"`
}

type CollectionView struct {
	HeaderString string `json:"headerString"`
}

func (data CollectionProductsData) Items() []Item {
	if len(data.CollectionProducts.Items) > 0 {
		return data.CollectionProducts.Items
	}
	return data.CollectionProductsBasedSearchResults.ItemResultList.FeaturedProducts
}

func (data CollectionProductsData) ItemIDs() []string {
	if len(data.CollectionProducts.ItemIDs) > 0 {
		return data.CollectionProducts.ItemIDs
	}
	return data.CollectionProductsBasedSearchResults.ItemResultList.ItemIDs
}

type ItemsPayload struct {
	Data   ItemsData      `json:"data"`
	Errors []GraphQLError `json:"errors"`
}

type ItemsData struct {
	Items []Item `json:"items"`
}

type GraphQLError struct {
	Message string `json:"message"`
	Path    []any  `json:"path"`
}

type Item struct {
	ID           string       `json:"id"`
	Name         string       `json:"name"`
	Size         string       `json:"size"`
	ProductID    string       `json:"productId"`
	LegacyID     string       `json:"legacyId"`
	BrandName    string       `json:"brandName"`
	BrandID      string       `json:"brandId"`
	EvergreenURL string       `json:"evergreenUrl"`
	Availability Availability `json:"availability"`
	ViewSection  ItemView     `json:"viewSection"`
	Price        ItemPrice    `json:"price"`
}

type Availability struct {
	Available  bool   `json:"available"`
	StockLevel string `json:"stockLevel"`
}

type ItemView struct {
	ItemImage          Image              `json:"itemImage"`
	TrackingProperties TrackingProperties `json:"trackingProperties"`
}

type Image struct {
	URL         string `json:"url"`
	TemplateURL string `json:"templateUrl"`
	AltText     string `json:"altText"`
}

type TrackingProperties struct {
	ProductID           string `json:"product_id"`
	ItemID              string `json:"item_id"`
	StockLevel          string `json:"stock_level"`
	Available           bool   `json:"available_ind"`
	ProductCategoryName string `json:"product_category_name"`
	ItemName            string `json:"item_name"`
}

type ItemPrice struct {
	ViewSection            ItemPriceView          `json:"viewSection"`
	ParWeightTotalEstimate ParWeightTotalEstimate `json:"parWeightTotalEstimate"`
}

type ItemPriceView struct {
	ItemCard             PriceDisplay `json:"itemCard"`
	ItemDetails          PriceDisplay `json:"itemDetails"`
	PriceString          string       `json:"priceString"`
	PriceValueString     string       `json:"priceValueString"`
	CurrencySymbolString string       `json:"currencySymbolString"`
}

type PriceDisplay struct {
	PriceAriaLabelString       string `json:"priceAriaLabelString"`
	PricePerUnitString         string `json:"pricePerUnitString"`
	PriceString                string `json:"priceString"`
	PricingUnitSecondaryString string `json:"pricingUnitSecondaryString"`
	PricingUnitString          string `json:"pricingUnitString"`
	FullPriceString            string `json:"fullPriceString"`
}

type ParWeightTotalEstimate struct {
	ViewSection ParWeightTotalEstimateView `json:"viewSection"`
}

type ParWeightTotalEstimateView struct {
	ParWeightString string `json:"parWeightString"`
}
