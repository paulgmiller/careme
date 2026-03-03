package types

// This has to be a subset of all providers
// also toon handles reflection metadata poorly so keep it simple?
type Ingredient struct {
	ProductId    string
	AisleNumber  string
	Brand        string
	Description  string
	PriceSale    string //blank means no sale
	PriceRegular string //3.50 dollars assumed?
	Size         string //ozs, lbs, etc
}
