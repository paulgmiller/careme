package query

// collectionProductsPayload matches the useful parts of ALDI's collection
// products GraphQL response.
type collectionProductsPayload struct {
	Data   collectionProductsData `json:"data"`
	Errors []graphQLError         `json:"errors"`
}

type collectionProductsData struct {
	CollectionProducts                   collectionProducts                   `json:"collectionProducts"`
	CollectionProductsBasedSearchResults collectionProductsBasedSearchResults `json:"collectionProductsBasedSearchResults"`
}

type collectionProducts struct {
	ItemIDs []string `json:"itemIds"`
	Items   []Item   `json:"items"`
}

type collectionProductsBasedSearchResults struct {
	ItemResultList searchItemResultList `json:"itemResultList"`
	SearchID       string               `json:"searchId"`
	ViewSection    collectionView       `json:"viewSection"`
}

type searchItemResultList struct {
	FeaturedProducts []Item   `json:"featuredProducts"`
	ItemIDs          []string `json:"itemIds"`
}

type collectionView struct {
	HeaderString string `json:"headerString"`
}

func (data collectionProductsData) items() []Item {
	if len(data.CollectionProducts.Items) > 0 {
		return data.CollectionProducts.Items
	}
	return data.CollectionProductsBasedSearchResults.ItemResultList.FeaturedProducts
}

func (data collectionProductsData) itemIDs() []string {
	if len(data.CollectionProducts.ItemIDs) > 0 {
		return data.CollectionProducts.ItemIDs
	}
	return data.CollectionProductsBasedSearchResults.ItemResultList.ItemIDs
}

type itemsPayload struct {
	Data   itemsData      `json:"data"`
	Errors []graphQLError `json:"errors"`
}

type itemsData struct {
	Items []Item `json:"items"`
}

type graphQLError struct {
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
	Availability availability `json:"availability"`
	ViewSection  itemView     `json:"viewSection"`
	Price        ItemPrice    `json:"price"`
}

type availability struct {
	Available  bool   `json:"available"`
	StockLevel string `json:"stockLevel"`
}

type itemView struct {
	ItemImage          image              `json:"itemImage"`
	TrackingProperties trackingProperties `json:"trackingProperties"`
}

type image struct {
	URL         string `json:"url"`
	TemplateURL string `json:"templateUrl"`
	AltText     string `json:"altText"`
}

type trackingProperties struct {
	ProductID           string `json:"product_id"`
	ItemID              string `json:"item_id"`
	StockLevel          string `json:"stock_level"`
	Available           bool   `json:"available_ind"`
	ProductCategoryName string `json:"product_category_name"`
	ItemName            string `json:"item_name"`
}

type ItemPrice struct {
	ViewSection            ItemPriceView          `json:"viewSection"`
	ParWeightTotalEstimate parWeightTotalEstimate `json:"parWeightTotalEstimate"`
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

type parWeightTotalEstimate struct {
	ViewSection parWeightTotalEstimateView `json:"viewSection"`
}

type parWeightTotalEstimateView struct {
	ParWeightString string `json:"parWeightString"`
}
