package query

// PathwaySearchPayload matches the Albertsons/Safeway pathway search response.
type PathwaySearchPayload struct {
	Response PathwaySearchResponse `json:"response"`
	//don't actually care about any of this just comment it out?
	OffersData     PathwaySearchOffersData         `json:"offersData"`
	Facet          PathwaySearchFacet              `json:"facet"`
	AppCode        string                          `json:"appCode"`
	AppMsg         string                          `json:"appMsg"`
	DynamicFilters map[string]PathwayDynamicFilter `json:"dynamic_filters"`
}

type PathwayDynamicFilter struct{}

type PathwaySearchResponse struct {
	NumFound        int                    `json:"numFound"`
	DisableTracking bool                   `json:"disableTracking"`
	Start           int                    `json:"start"`
	MiscInfo        PathwaySearchMiscInfo  `json:"miscInfo"`
	IsExactMatch    bool                   `json:"isExactMatch"`
	Docs            []PathwaySearchProduct `json:"docs"`
}

type PathwaySearchMiscInfo struct {
	AttributionToken string `json:"attributionToken"`
	Query            string `json:"query"`
	Sort             string `json:"sort"`
	Filter           string `json:"filter"`
	NextPageToken    string `json:"nextPageToken"`
}

type PathwaySearchProduct struct {
	Status                    string                    `json:"status"`
	Name                      string                    `json:"name"`
	PID                       string                    `json:"pid"`
	UPC                       string                    `json:"upc"`
	ID                        string                    `json:"id"`
	StoreID                   string                    `json:"storeId"`
	Featured                  bool                      `json:"featured"`
	IsDYOCake                 bool                      `json:"isDYOCake"`
	InventoryAvailable        string                    `json:"inventoryAvailable"`
	PastPurchased             bool                      `json:"pastPurchased"`
	RestrictedValue           string                    `json:"restrictedValue"`
	SalesRank                 int                       `json:"salesRank"`
	AgreementID               int                       `json:"agreementId"`
	FeaturedProductID         int                       `json:"featuredProductId"`
	ImageURL                  string                    `json:"imageUrl"`
	Price                     float64                   `json:"price"`
	PromoDescription          string                    `json:"promoDescription"`
	PromoText                 string                    `json:"promoText"`
	PromoType                 string                    `json:"promoType"`
	PromoEndDate              *string                   `json:"promoEndDate"`
	BasePrice                 float64                   `json:"basePrice"`
	BasePricePer              float64                   `json:"basePricePer"`
	PricePer                  float64                   `json:"pricePer"`
	DisplayType               string                    `json:"displayType"`
	AisleID                   string                    `json:"aisleId"`
	AisleName                 string                    `json:"aisleName"`
	DepartmentName            string                    `json:"departmentName"`
	ShelfName                 string                    `json:"shelfName"`
	ShelfNameWithID           string                    `json:"shelfNameWithId"`
	AisleLocation             string                    `json:"aisleLocation"`
	ShelfXCoordinateNbr       *string                   `json:"shelfXcoordinateNbr"`
	ShelfYCoordinateNbr       *string                   `json:"shelfYcoordinateNbr"`
	SlotXCoordinateNbr        *string                   `json:"slotXcoordinateNbr"`
	SlotYCoordinateNbr        *string                   `json:"slotYcoordinateNbr"`
	FixtureXCoordinateNbr     *string                   `json:"fixtureXcoordinateNbr"`
	FixtureYCoordinateNbr     *string                   `json:"fixtureYcoordinateNbr"`
	AislePositionTxt          *string                   `json:"aislePositionTxt"`
	ShelfDimensionDpth        *string                   `json:"shelfDimensionDpth"`
	SnapEligible              bool                      `json:"snapEligible"`
	UnitOfMeasure             string                    `json:"unitOfMeasure"`
	SellByWeight              string                    `json:"sellByWeight"`
	AverageWeight             []string                  `json:"averageWeight"`
	UnitQuantity              string                    `json:"unitQuantity"`
	DisplayUnitQuantityText   *string                   `json:"displayUnitQuantityText"`
	DisplayEstimateText       *string                   `json:"displayEstimateText"`
	PreviousPurchaseQty       int                       `json:"previousPurchaseQty"`
	MaxPurchaseQty            int                       `json:"maxPurchaseQty"`
	MinWeight                 string                    `json:"minWeight"`
	MaxWeight                 string                    `json:"maxWeight"`
	IsHhcProduct              bool                      `json:"isHhcProduct"`
	Prop65WarningTypeCD       string                    `json:"prop65WarningTypeCD"`
	Prop65WarningText         string                    `json:"prop65WarningText"`
	Prop65WarningIconRequired bool                      `json:"prop65WarningIconRequired"`
	IsArProduct               bool                      `json:"isArProduct"`
	IsMtoProduct              bool                      `json:"isMtoProduct"`
	IsCustomizable            bool                      `json:"isCustomizable"`
	InStoreShoppingElig       bool                      `json:"inStoreShoppingElig"`
	PreparationTime           string                    `json:"preparationTime"`
	IsMarketplaceItem         string                    `json:"isMarketplaceItem"`
	AlgoType                  string                    `json:"algoType"`
	TriggerQuantity           int                       `json:"triggerQuantity"`
	IDOfAisle                 string                    `json:"idOfAisle"`
	IDOfShelf                 string                    `json:"idOfShelf"`
	IDOfDepartment            string                    `json:"idOfDepartment"`
	Warnings                  []PathwaySearchWarning    `json:"warnings"`
	RequiresReturn            bool                      `json:"requiresReturn"`
	ChannelEligibility        PathwayChannelEligibility `json:"channelEligibility"`
	ChannelInventory          PathwayChannelInventory   `json:"channelInventory"`
	ProductReview             PathwayProductReview      `json:"productReview"`
	ItemSizeQty               string                    `json:"itemSizeQty"`
	ItemPackageQty            string                    `json:"itemPackageQty"`
	BestSellerRank            *int                      `json:"bestSellerRank"`
	ItemRetailSect            string                    `json:"itemRetailSect"`
	Badges                    []PathwaySearchBadge      `json:"badges"`
	Labels                    []PathwaySearchLabel      `json:"labels"`
}

type PathwaySearchWarning struct {
	FoodIndicator   string `json:"foodIndicator"`
	WarnMsgTxt      string `json:"warnMsgTxt"`
	WarningSourceNm string `json:"warningSourceNm"`
}

type PathwayChannelEligibility struct {
	PickUp   bool `json:"pickUp"`
	Delivery bool `json:"delivery"`
	InStore  bool `json:"inStore"`
	Shipping bool `json:"shipping"`
}

type PathwayChannelInventory struct {
	Delivery string `json:"delivery"`
	Pickup   string `json:"pickup"`
	Instore  string `json:"instore"`
	Shipping string `json:"shipping"`
}

type PathwayProductReview struct {
	AvgRating               string `json:"avgRating"`
	ReviewCount             string `json:"reviewCount"`
	IsReviewWriteEligible   string `json:"isReviewWriteEligible"`
	IsReviewDisplayEligible string `json:"isReviewDisplayEligible"`
	IsForOnetimeReview      string `json:"isForOnetimeReview"`
	ReviewTemplateType      string `json:"reviewTemplateType"`
}

type PathwaySearchBadge struct {
	BadgeName       string `json:"badgeName"`
	Color           string `json:"color"`
	IsStrikethrough bool   `json:"isStrikethrough"`
	IsBoldText      bool   `json:"isBoldText"`
	Icon            string `json:"icon"`
}

type PathwaySearchLabel struct {
	LabelName string `json:"labelName"`
}

type PathwaySearchOffersData struct {
	Departments map[string]PathwayDepartmentOffers `json:"departments"`
	UPCs        map[string]PathwayUPCOffers        `json:"upcs"`
}

type PathwayDepartmentOffers struct {
	Offers map[string]PathwayOffer `json:"offers"`
}

type PathwayUPCOffers struct {
	WeeklyAdBadge bool                    `json:"weeklyAdBadge"`
	Offers        map[string]PathwayOffer `json:"offers,omitempty"`
}

type PathwayOffer struct {
	Deleted          bool    `json:"deleted"`
	DeliveryChannel  string  `json:"deliveryChannel"`
	EcomDescription  string  `json:"ecomDescription"`
	EndDate          string  `json:"endDate"`
	ExtlOfferID      string  `json:"extlOfferId"`
	IsClippable      bool    `json:"isClippable"`
	IsDisplayable    bool    `json:"isDisplayable"`
	MaxPurchaseQty   int     `json:"maxPurchaseQty"`
	MinPurchaseQty   int     `json:"minPurchaseQty"`
	OfferID          string  `json:"offerId"`
	OfferPGM         string  `json:"offerPgm"`
	OfferProgramType string  `json:"offerProgramType"`
	OfferProtoType   string  `json:"offerProtoType"`
	OfferSubPGM      string  `json:"offerSubPgm"`
	OfferTS          string  `json:"offerTs"`
	Price            float64 `json:"price"`
	PurchaseInd      string  `json:"purchaseInd"`
	StartDate        string  `json:"startDate"`
}

type PathwaySearchFacet struct {
	Ranges        []PathwayFacetRange   `json:"ranges"`
	Fields        []PathwayFacetField   `json:"fields"`
	DynamicFacets []PathwayDynamicFacet `json:"dynamic_facets"`
}

type PathwayFacetRange struct {
	Key   string                  `json:"key"`
	Value []PathwayFacetRangeBand `json:"value"`
}

type PathwayFacetRangeBand struct {
	Count int    `json:"count"`
	Start string `json:"start"`
	End   string `json:"end"`
}

type PathwayFacetField struct {
	Key   string                  `json:"key"`
	Value []PathwayFacetFieldItem `json:"value"`
}

type PathwayFacetFieldItem struct {
	Count string `json:"count"`
	Name  string `json:"name"`
}

type PathwayDynamicFacet struct {
	FacetID     string                   `json:"facetId"`
	DisplayText string                   `json:"displayText"`
	Filters     []PathwayDynamicFacetHit `json:"filters"`
}

type PathwayDynamicFacetHit struct {
	Count string `json:"count"`
	Name  string `json:"name"`
}
