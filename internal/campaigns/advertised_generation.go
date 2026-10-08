package campaigns

import (
	"fmt"

	"careme/internal/locations"
)

// Campaign is a promoted store plus campaign-specific page context.
type campaign struct {
	Location    locations.Location
	HelpMessage string
	Generate    bool
}

func genericLocationHelp(location string) string {
	return fmt.Sprintf(`Here are 3 recipes made with ingredients in stock today at %s.
Want something different? Add a note below and choose Try again, chef. 
Add the recipes you like, hide the ones you don't, and we'll build your shopping list.`, location)
}

// AdvertisedRecipeLocations returns promoted stores and whether to pre-generate their recipes.
// should probably vagule align with StaplesWatchdogLocations() as why wouldn't we monitor
// the most importnant stores
func AdvertisedRecipeLocations() map[string]campaign {
	return map[string]campaign{
		// https://chatgpt.com/share/6a4d4793-987c-83e8-9f0e-e12c72772df7
		// bella bottega qfc 70500860 98052
		/* soon
		"university_village_qfc": {
			Location:    locations.Location{ID: "70500807", ZipCode: "98105"},
			HelpMessage: genericLocationHelp("University Village QFC"),
		},*/
		"westlake_wf": {
			Location:    locations.Location{ID: "wholefoods_10216"},
			HelpMessage: genericLocationHelp("Westlake Whole Foods"),
			Generate:    true,
		},
		// These delivery campaigns should probably only generate weekly.
		"smithbrothersfarms": {
			Location: locations.Location{ID: "smithbrothersfarms_delivery"},
			Generate: false,
		},
		"mnfoodclub": {
			Location: locations.Location{ID: "mnfoodclub_delivery"},
			Generate: false,
		},
		/* Too close to fred meyers
		bellevue_wf": {
			Location:    locations.Location{ID: "wholefoods_10153", ZipCode: "98004"},
			HelpMessage: genericLocationHelp("Bellevue Whole Foods"),
		},*/
		"bellevue": {
			Location:    locations.Location{ID: "70100023"},
			HelpMessage: genericLocationHelp("Bellevue Fred Meyer"),
			Generate:    true,
		},
		"issaquah": {
			Location:    locations.Location{ID: "70100658"},
			HelpMessage: genericLocationHelp("Issaquah Fred Meyer"),
			Generate:    true,
		},
	}
}
