package googleads

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClientSearchCampaignProximitiesSendsHeadersAndParsesStringMicrodegrees(t *testing.T) {
	var sawSearch bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth":
			require.Equal(t, "application/x-www-form-urlencoded", r.Header.Get("Content-Type"))
			_, _ = w.Write([]byte(`{"access_token":"token-1","expires_in":3600}`))
		case "/v24/customers/123/googleAds:search":
			sawSearch = true
			assert.Equal(t, "Bearer token-1", r.Header.Get("Authorization"))
			assert.Equal(t, "dev-token", r.Header.Get("developer-token"))
			assert.Equal(t, "9998887777", r.Header.Get("login-customer-id"))
			var req searchRequest
			require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
			assert.Contains(t, req.Query, "campaign.id = 456")
			_, _ = w.Write([]byte(`{
				"results": [{
					"campaignCriterion": {
						"resourceName": "customers/123/campaignCriteria/456~1",
						"proximity": {
							"geoPoint": {
								"latitudeInMicroDegrees": "47610000",
								"longitudeInMicroDegrees": "-122200000"
							},
							"radius": 2,
							"radiusUnits": "MILES"
						}
					}
				}]
			}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	client.oauthURL = server.URL + "/oauth"

	criteria, err := client.SearchCampaignProximities(context.Background(), "123", "456")
	require.NoError(t, err)
	require.True(t, sawSearch)
	require.Len(t, criteria, 1)
	assert.Equal(t, int64(47610000), criteria[0].LatMicro)
	assert.Equal(t, int64(-122200000), criteria[0].LonMicro)
}

func TestConfigFromEnv(t *testing.T) {
	t.Setenv("GOOGLE_ADS_DEVELOPER_TOKEN", "dev")
	t.Setenv("GOOGLE_ADS_CLIENT_ID", "client")
	t.Setenv("GOOGLE_ADS_CLIENT_SECRET", "secret")
	t.Setenv("GOOGLE_ADS_REFRESH_TOKEN", "refresh")
	t.Setenv("GOOGLE_ADS_LOGIN_CUSTOMER_ID", "login")

	cfg := ConfigFromEnv()

	assert.Equal(t, "dev", cfg.DeveloperToken)
	assert.Equal(t, "client", cfg.ClientID)
	assert.Equal(t, "secret", cfg.ClientSecret)
	assert.Equal(t, "refresh", cfg.RefreshToken)
	assert.Equal(t, "login", cfg.LoginCustomerID)
}

func TestClientCreateAndRemoveCampaignCriteriaRequestShapes(t *testing.T) {
	var sawCreate bool
	var sawRemove bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth":
			_, _ = w.Write([]byte(`{"access_token":"token-1","expires_in":3600}`))
		case "/v24/customers/123/campaignCriteria:mutate":
			var raw map[string][]map[string]any
			require.NoError(t, json.NewDecoder(r.Body).Decode(&raw))
			ops := raw["operations"]
			require.Len(t, ops, 1)
			if create, ok := ops[0]["create"].(map[string]any); ok {
				sawCreate = true
				assert.Equal(t, "customers/123/campaigns/456", create["campaign"])
				proximity := create["proximity"].(map[string]any)
				assert.Equal(t, "MILES", proximity["radiusUnits"])
				assert.Equal(t, float64(2), proximity["radius"])
				geoPoint := proximity["geoPoint"].(map[string]any)
				assert.Equal(t, float64(47610000), geoPoint["latitudeInMicroDegrees"])
				_, _ = w.Write([]byte(`{"results":[{"resourceName":"customers/123/campaignCriteria/456~1"}]}`))
				return
			}
			if remove, ok := ops[0]["remove"].(string); ok {
				sawRemove = true
				assert.True(t, strings.HasSuffix(remove, "/456~1"))
				_, _ = w.Write([]byte(`{"results":[{"resourceName":"customers/123/campaignCriteria/456~1"}]}`))
				return
			}
			t.Fatalf("unexpected operations %#v", ops)
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	client.oauthURL = server.URL + "/oauth"
	created, err := client.CreateProximityCriteria(context.Background(), "123", "456", []Target{{
		StoreID:     "11111111",
		LatMicro:    47610000,
		LonMicro:    -122200000,
		RadiusMiles: 2,
	}})
	require.NoError(t, err)
	assert.Equal(t, []string{"customers/123/campaignCriteria/456~1"}, created)

	err = client.RemoveCampaignCriteria(context.Background(), "123", []string{"customers/123/campaignCriteria/456~1"})
	require.NoError(t, err)
	assert.True(t, sawCreate)
	assert.True(t, sawRemove)
}

func TestClientSearchAdGroups(t *testing.T) {
	var sawSearch bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth":
			_, _ = w.Write([]byte(`{"access_token":"token-1","expires_in":3600}`))
		case "/v24/customers/123/googleAds:search":
			sawSearch = true
			var req searchRequest
			require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
			assert.Contains(t, req.Query, "campaign.id = 456")
			assert.Contains(t, req.Query, "ad_group.status != REMOVED")
			_, _ = w.Write([]byte(`{
				"results": [{
					"adGroup": {
						"resourceName": "customers/123/adGroups/77",
						"name": "Careme Store 11111111 Store One"
					}
				}]
			}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	client.oauthURL = server.URL + "/oauth"

	adGroups, err := client.SearchAdGroups(context.Background(), "123", "456")
	require.NoError(t, err)
	require.True(t, sawSearch)
	assert.Equal(t, []AdGroupSummary{{
		ResourceName: "customers/123/adGroups/77",
		Name:         "Careme Store 11111111 Store One",
	}}, adGroups)
}

func TestClientCreateStoreAdGroupResourcesRequestShapes(t *testing.T) {
	paths := make(map[string]int)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths[r.URL.Path]++
		switch r.URL.Path {
		case "/oauth":
			_, _ = w.Write([]byte(`{"access_token":"token-1","expires_in":3600}`))
		case "/v24/customers/123/adGroups:mutate":
			var raw map[string][]map[string]any
			require.NoError(t, json.NewDecoder(r.Body).Decode(&raw))
			create := raw["operations"][0]["create"].(map[string]any)
			assert.Equal(t, "customers/123/campaigns/456", create["campaign"])
			assert.Equal(t, "Careme Store 11111111 Kroger One", create["name"])
			assert.Equal(t, "ENABLED", create["status"])
			_, _ = w.Write([]byte(`{"results":[{"resourceName":"customers/123/adGroups/77"}]}`))
		case "/v24/customers/123/adGroupCriteria:mutate":
			var raw map[string][]map[string]any
			require.NoError(t, json.NewDecoder(r.Body).Decode(&raw))
			create := raw["operations"][0]["create"].(map[string]any)
			assert.Equal(t, "customers/123/adGroups/77", create["adGroup"])
			proximity := create["proximity"].(map[string]any)
			assert.Equal(t, "MILES", proximity["radiusUnits"])
			_, _ = w.Write([]byte(`{"results":[{"resourceName":"customers/123/adGroupCriteria/77~88"}]}`))
		case "/v24/customers/123/adGroupAds:mutate":
			var raw map[string][]map[string]any
			require.NoError(t, json.NewDecoder(r.Body).Decode(&raw))
			create := raw["operations"][0]["create"].(map[string]any)
			assert.Equal(t, "customers/123/adGroups/77", create["adGroup"])
			assert.Equal(t, "PAUSED", create["status"])
			ad := create["ad"].(map[string]any)
			assert.Equal(t, []any{"https://careme.cooking/recipes?location=11111111"}, ad["finalUrls"])
			_, _ = w.Write([]byte(`{"results":[{"resourceName":"customers/123/adGroupAds/77~99"}]}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	client.oauthURL = server.URL + "/oauth"
	targets := []Target{{
		StoreID:     "11111111",
		StoreName:   "Kroger One",
		LatMicro:    47610000,
		LonMicro:    -122200000,
		RadiusMiles: 2,
	}}

	adGroups, err := client.CreateAdGroups(context.Background(), "123", "456", targets)
	require.NoError(t, err)
	criteria, err := client.CreateAdGroupProximityCriteria(context.Background(), "123", targets, adGroups)
	require.NoError(t, err)
	ads, err := client.CreateResponsiveSearchAds(context.Background(), "123", []StoreAd{{
		AdGroup:      adGroups[0],
		FinalURL:     "https://careme.cooking/recipes?location=11111111",
		Status:       "PAUSED",
		Headlines:    []string{"Dinner ideas nearby", "Cook fresh tonight", "Fresh meal ideas"},
		Descriptions: []string{"Get recipe ideas built around groceries near you.", "Find simple meals and build a grocery list for this store."},
	}})
	require.NoError(t, err)

	assert.Equal(t, []string{"customers/123/adGroups/77"}, adGroups)
	assert.Equal(t, []string{"customers/123/adGroupCriteria/77~88"}, criteria)
	assert.Equal(t, []string{"customers/123/adGroupAds/77~99"}, ads)
	assert.Equal(t, 1, paths["/v24/customers/123/adGroups:mutate"])
	assert.Equal(t, 1, paths["/v24/customers/123/adGroupCriteria:mutate"])
	assert.Equal(t, 1, paths["/v24/customers/123/adGroupAds:mutate"])
}

func newTestClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	client, err := NewClient(Config{
		DeveloperToken:  "dev-token",
		ClientID:        "client-id",
		ClientSecret:    "client-secret",
		RefreshToken:    "refresh-token",
		LoginCustomerID: "999-888-7777",
		BaseURL:         baseURL,
	})
	require.NoError(t, err)
	return client
}
