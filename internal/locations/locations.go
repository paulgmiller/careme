package locations

import (
	"bytes"
	"careme/internal/config"
	"careme/internal/html"
	"careme/internal/kroger"
	"context"
	"fmt"
	"log"
	"sync"
	"embed"
	"html/template"
)

//go:embed templates/*.html
var templatesFS embed.FS

var templates = template.Must(template.New("").ParseFS(templatesFS, "templates/*.html"))

// this should all be in a location service object
var locationCache map[string]Location
var cacheLock sync.Mutex // to protect locationMap

func init() {
	locationCache = make(map[string]Location)
}

func GetLocationByID(ctx context.Context, cfg *config.Config, locationID string) (*Location, error) {

	cacheLock.Lock()

	if loc, exists := locationCache[locationID]; exists {
		cacheLock.Unlock()
		return &loc, nil
	}
	cacheLock.Unlock()

	client, err := kroger.FromConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kroger client: %w", err)
	}

	resp, err := client.LocationDetailsWithResponse(ctx, locationID)
	if err != nil {
		return nil, fmt.Errorf("failed to get location details for ID %s: %w", locationID, err)
	}

	if resp.JSON200 == nil || resp.JSON200.Data == nil {
		return nil, fmt.Errorf("no data found for location ID %s", locationID)
	}

	l := Location{
		ID:      locationID,
		Name:    *resp.JSON200.Data.Name,
		Address: *resp.JSON200.Data.Address.AddressLine1,
	}
	cacheLock.Lock()
	defer cacheLock.Unlock()
	locationCache[locationID] = l
	return &l, nil
}

func Html(cfg *config.Config, locs []Location, zipstring string) string {
	data := struct {
		Locations []Location
		Zip       string
		ClarityScript template.HTML
	}{
		Locations: locs,
		Zip:       zipstring,
		ClarityScript: html.ClarityScript(cfg),
	}
	var buf bytes.Buffer
	_ = templates.ExecuteTemplate(&buf, "locations.html", data)
	return buf.String()
}

type Location struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Address string `json:"address"`
	State   string `json:"state"`
}

func GetLocationsByZip(ctx context.Context, cfg *config.Config, zipcode string) ([]Location, error) {
	client, err := kroger.FromConfig(ctx, cfg)
	if err != nil {
		log.Fatalf("failed to create Kroger client: %v", err)
	}
	locparams := &kroger.LocationListParams{
		FilterZipCodeNear: &zipcode,
	}
	resp, err := client.LocationListWithResponse(ctx, locparams)
	if err != nil {
		log.Fatalf("failed to get locations for zip %s: %v", zipcode, err)
	}
	if resp.JSON200 == nil || len(*resp.JSON200.Data) == 0 {
		fmt.Printf("No locations found for zip code %s\n", zipcode)
		return nil, nil
	}

	var locations []Location
	cacheLock.Lock()
	defer cacheLock.Unlock()
	for _, loc := range *resp.JSON200.Data {
		l := Location{
			ID:      *loc.LocationId,
			Name:    *loc.Name,
			Address: *loc.Address.AddressLine1,
			State:   *loc.Address.State,
		}
		locationCache[l.ID] = l
		locations = append(locations, l)
	}
	return locations, nil
}
