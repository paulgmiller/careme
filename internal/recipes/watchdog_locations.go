package recipes

import "careme/internal/locations"

// StaplesWatchdogLocations returns the stores checked by the staples watchdog.
func StaplesWatchdogLocations() []locations.Location {
	return []locations.Location{
		{ID: "wholefoods_10153", ZipCode: "97209"},
		{ID: "safeway_490", ZipCode: "86403"},
		{ID: "70500874", ZipCode: "98101"},
		{ID: "starmarket_3566", ZipCode: "02108"},
		{ID: "acmemarkets_806", ZipCode: "19711"},
		{ID: "publix_1847", ZipCode: "35401"},
		{ID: "aldi_F219", ZipCode: "40222"},
		{ID: "heb_540", ZipCode: "77023"},
	}
}
