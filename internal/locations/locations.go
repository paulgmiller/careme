package locations

import (
	"careme/internal/config"
	"careme/internal/kroger"
	"context"
	"fmt"
	"html"
	"log"
	"strings"
	"sync"
)

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

func Html(locs []Location, zipstring string) string {
	var b strings.Builder
	b.WriteString("<!DOCTYPE html><html><head><meta charset='utf-8'><title>Careme Locations</title>")
	b.WriteString(`<style>body{font-family:system-ui,-apple-system,sans-serif;margin:2rem;background:#f5f7fa;color:#222}pre{background:#111;color:#eee;padding:1rem;border-radius:8px;overflow-x:auto;white-space:pre-wrap;word-break:break-word;font-size:.9rem;line-height:1.3}form{margin-bottom:1.5rem;display:flex;gap:.5rem}input[type=text]{flex:1;padding:.6rem .8rem;border:1px solid #bbb;border-radius:4px;font-size:1rem}button{background:#2563eb;color:#fff;border:0;padding:.6rem 1rem;border-radius:4px;cursor:pointer;font-size:1rem}button:hover{background:#1d4ed8}h1{margin-top:0}footer{margin-top:2rem;font-size:.75rem;color:#666}</style>`)
	b.WriteString("</head><body>")
	b.WriteString("<h1>Locations near " + html.EscapeString(zipstring) + "</h1>")
	b.WriteString("<ul>")
	for _, l := range locs {
		b.WriteString(fmt.Sprintf("<li><a href='/recipes?location=%s'>%s, %s</a></li>", html.EscapeString(l.ID), html.EscapeString(l.Name), html.EscapeString(l.Address)))
	}
	b.WriteString("</ul></body></html>")
	return b.String()
}

type Location struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Address string `json:"address"`
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
		}
		locationCache[l.ID] = l
		locations = append(locations, l)
	}
	return locations, nil
}
