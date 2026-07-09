package googleads

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"careme/internal/config"
)

const (
	defaultBaseURL  = "https://googleads.googleapis.com"
	defaultOAuthURL = "https://www.googleapis.com/oauth2/v3/token"
	defaultVersion  = "v24"
)

type Config struct {
	DeveloperToken  string
	ClientID        string
	ClientSecret    string
	RefreshToken    string
	LoginCustomerID string
	BaseURL         string
	OAuthURL        string
	Version         string
	HTTPClient      *http.Client
}

func ConfigFromApp(cfg config.GoogleAdsConfig) Config {
	return Config{
		DeveloperToken:  cfg.DeveloperToken,
		ClientID:        cfg.ClientID,
		ClientSecret:    cfg.ClientSecret,
		RefreshToken:    cfg.RefreshToken,
		LoginCustomerID: cfg.LoginCustomerID,
	}
}

func (c Config) validate() error {
	var missing []string
	if strings.TrimSpace(c.DeveloperToken) == "" {
		missing = append(missing, "GOOGLE_ADS_DEVELOPER_TOKEN")
	}
	if strings.TrimSpace(c.ClientID) == "" {
		missing = append(missing, "GOOGLE_ADS_CLIENT_ID")
	}
	if strings.TrimSpace(c.ClientSecret) == "" {
		missing = append(missing, "GOOGLE_ADS_CLIENT_SECRET")
	}
	if strings.TrimSpace(c.RefreshToken) == "" {
		missing = append(missing, "GOOGLE_ADS_REFRESH_TOKEN")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing Google Ads configuration: %s", strings.Join(missing, ", "))
	}
	return nil
}

type Client struct {
	cfg       Config
	http      *http.Client
	baseURL   string
	oauthURL  string
	version   string
	token     string
	expiresAt time.Time
	mu        sync.Mutex
}

func NewClient(cfg Config) (*Client, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	oauthURL := strings.TrimSpace(cfg.OAuthURL)
	if oauthURL == "" {
		oauthURL = defaultOAuthURL
	}
	version := strings.Trim(strings.TrimSpace(cfg.Version), "/")
	if version == "" {
		version = defaultVersion
	}
	return &Client{
		cfg:      cfg,
		http:     httpClient,
		baseURL:  baseURL,
		oauthURL: oauthURL,
		version:  version,
	}, nil
}

type ProximityCriterion struct {
	ResourceName string
	LatMicro     int64
	LonMicro     int64
	RadiusMiles  float64
}

type AdGroupSummary struct {
	ResourceName string
	Name         string
}

func (c *Client) SearchCampaignProximities(ctx context.Context, customerID, campaignID string) ([]ProximityCriterion, error) {
	query := fmt.Sprintf(`SELECT campaign_criterion.resource_name, campaign_criterion.proximity.geo_point.latitude_in_micro_degrees, campaign_criterion.proximity.geo_point.longitude_in_micro_degrees, campaign_criterion.proximity.radius, campaign_criterion.proximity.radius_units FROM campaign_criterion WHERE campaign.id = %s AND campaign_criterion.type = PROXIMITY AND campaign_criterion.status != REMOVED`, campaignID)
	var response searchResponse
	if err := c.doJSON(ctx, http.MethodPost, fmt.Sprintf("/customers/%s/googleAds:search", customerID), searchRequest{Query: query}, &response); err != nil {
		return nil, err
	}

	criteria := make([]ProximityCriterion, 0, len(response.Results))
	for _, result := range response.Results {
		criterion := result.CampaignCriterion
		if criterion.Proximity.RadiusUnits != "" && criterion.Proximity.RadiusUnits != "MILES" {
			continue
		}
		criteria = append(criteria, ProximityCriterion{
			ResourceName: criterion.ResourceName,
			LatMicro:     int64(criterion.Proximity.GeoPoint.LatitudeInMicroDegrees),
			LonMicro:     int64(criterion.Proximity.GeoPoint.LongitudeInMicroDegrees),
			RadiusMiles:  criterion.Proximity.Radius,
		})
	}
	return criteria, nil
}

func (c *Client) SearchAdGroups(ctx context.Context, customerID, campaignID string) ([]AdGroupSummary, error) {
	query := fmt.Sprintf(`SELECT ad_group.resource_name, ad_group.name FROM ad_group WHERE campaign.id = %s AND ad_group.status != REMOVED`, campaignID)
	var response searchResponse
	if err := c.doJSON(ctx, http.MethodPost, fmt.Sprintf("/customers/%s/googleAds:search", customerID), searchRequest{Query: query}, &response); err != nil {
		return nil, err
	}

	adGroups := make([]AdGroupSummary, 0, len(response.Results))
	for _, result := range response.Results {
		adGroups = append(adGroups, AdGroupSummary{
			ResourceName: result.AdGroup.ResourceName,
			Name:         result.AdGroup.Name,
		})
	}
	return adGroups, nil
}

func (c *Client) CreateProximityCriteria(ctx context.Context, customerID, campaignID string, targets []Target) ([]string, error) {
	if len(targets) == 0 {
		return nil, nil
	}
	operations := make([]campaignCriterionOperation, 0, len(targets))
	campaign := fmt.Sprintf("customers/%s/campaigns/%s", customerID, campaignID)
	for _, target := range targets {
		operations = append(operations, campaignCriterionOperation{
			Create: &campaignCriterion{
				Campaign: campaign,
				Proximity: &proximityInfo{
					GeoPoint: &geoPointInfo{
						LatitudeInMicroDegrees:  microDegrees(target.LatMicro),
						LongitudeInMicroDegrees: microDegrees(target.LonMicro),
					},
					Radius:      target.RadiusMiles,
					RadiusUnits: "MILES",
				},
			},
		})
	}
	var response mutateCampaignCriteriaResponse
	if err := c.doJSON(ctx, http.MethodPost, fmt.Sprintf("/customers/%s/campaignCriteria:mutate", customerID), mutateCampaignCriteriaRequest{Operations: operations}, &response); err != nil {
		return nil, err
	}

	resourceNames := make([]string, 0, len(response.Results))
	for _, result := range response.Results {
		resourceNames = append(resourceNames, result.ResourceName)
	}
	return resourceNames, nil
}

func (c *Client) RemoveCampaignCriteria(ctx context.Context, customerID string, resourceNames []string) error {
	if len(resourceNames) == 0 {
		return nil
	}
	operations := make([]campaignCriterionOperation, 0, len(resourceNames))
	for _, resourceName := range resourceNames {
		operations = append(operations, campaignCriterionOperation{Remove: resourceName})
	}
	return c.doJSON(ctx, http.MethodPost, fmt.Sprintf("/customers/%s/campaignCriteria:mutate", customerID), mutateCampaignCriteriaRequest{Operations: operations}, nil)
}

func (c *Client) CreateAdGroups(ctx context.Context, customerID, campaignID string, targets []Target) ([]string, error) {
	if len(targets) == 0 {
		return nil, nil
	}
	operations := make([]adGroupOperation, 0, len(targets))
	campaign := fmt.Sprintf("customers/%s/campaigns/%s", customerID, campaignID)
	for _, target := range targets {
		operations = append(operations, adGroupOperation{
			Create: &adGroup{
				Campaign: campaign,
				Name:     AdGroupName(target),
				Status:   "ENABLED",
				Type:     "SEARCH_STANDARD",
			},
		})
	}
	var response mutateResponse
	if err := c.doJSON(ctx, http.MethodPost, fmt.Sprintf("/customers/%s/adGroups:mutate", customerID), mutateAdGroupsRequest{Operations: operations}, &response); err != nil {
		return nil, err
	}
	return mutationResourceNames(response), nil
}

func (c *Client) RemoveAdGroups(ctx context.Context, customerID string, resourceNames []string) error {
	if len(resourceNames) == 0 {
		return nil
	}
	operations := make([]adGroupOperation, 0, len(resourceNames))
	for _, resourceName := range resourceNames {
		operations = append(operations, adGroupOperation{Remove: resourceName})
	}
	return c.doJSON(ctx, http.MethodPost, fmt.Sprintf("/customers/%s/adGroups:mutate", customerID), mutateAdGroupsRequest{Operations: operations}, nil)
}

func (c *Client) CreateAdGroupProximityCriteria(ctx context.Context, customerID string, targets []Target, adGroups []string) ([]string, error) {
	if len(targets) == 0 {
		return nil, nil
	}
	if len(targets) != len(adGroups) {
		return nil, fmt.Errorf("create ad group proximity criteria for %d targets with %d ad groups", len(targets), len(adGroups))
	}
	operations := make([]adGroupCriterionOperation, 0, len(targets))
	for i, target := range targets {
		operations = append(operations, adGroupCriterionOperation{
			Create: &adGroupCriterion{
				AdGroup: adGroups[i],
				Status:  "ENABLED",
				Proximity: &proximityInfo{
					GeoPoint: &geoPointInfo{
						LatitudeInMicroDegrees:  microDegrees(target.LatMicro),
						LongitudeInMicroDegrees: microDegrees(target.LonMicro),
					},
					Radius:      target.RadiusMiles,
					RadiusUnits: "MILES",
				},
			},
		})
	}
	var response mutateResponse
	if err := c.doJSON(ctx, http.MethodPost, fmt.Sprintf("/customers/%s/adGroupCriteria:mutate", customerID), mutateAdGroupCriteriaRequest{Operations: operations}, &response); err != nil {
		return nil, err
	}
	return mutationResourceNames(response), nil
}

func (c *Client) CreateResponsiveSearchAds(ctx context.Context, customerID string, ads []StoreAd) ([]string, error) {
	if len(ads) == 0 {
		return nil, nil
	}
	operations := make([]adGroupAdOperation, 0, len(ads))
	for _, ad := range ads {
		operations = append(operations, adGroupAdOperation{
			Create: &adGroupAd{
				AdGroup: ad.AdGroup,
				Status:  ad.Status,
				Ad: responsiveSearchAdWrapper{
					FinalURLs: []string{ad.FinalURL},
					ResponsiveSearchAd: responsiveSearchAd{
						Headlines:    adTextAssets(ad.Headlines),
						Descriptions: adTextAssets(ad.Descriptions),
					},
				},
			},
		})
	}
	var response mutateResponse
	if err := c.doJSON(ctx, http.MethodPost, fmt.Sprintf("/customers/%s/adGroupAds:mutate", customerID), mutateAdGroupAdsRequest{Operations: operations}, &response); err != nil {
		return nil, err
	}
	return mutationResourceNames(response), nil
}

func (c *Client) doJSON(ctx context.Context, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+"/"+c.version+path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if err := c.authorize(ctx, req); err != nil {
		return err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("google ads %s %s failed: status %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decode google ads response: %w", err)
	}
	return nil
}

func (c *Client) authorize(ctx context.Context, req *http.Request) error {
	token, err := c.accessToken(ctx)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("developer-token", c.cfg.DeveloperToken)
	if loginCustomerID := strings.TrimSpace(c.cfg.LoginCustomerID); loginCustomerID != "" {
		req.Header.Set("login-customer-id", strings.ReplaceAll(loginCustomerID, "-", ""))
	}
	return nil
}

func (c *Client) accessToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && time.Now().Before(c.expiresAt.Add(-time.Minute)) {
		return c.token, nil
	}

	values := url.Values{}
	values.Set("grant_type", "refresh_token")
	values.Set("client_id", c.cfg.ClientID)
	values.Set("client_secret", c.cfg.ClientSecret)
	values.Set("refresh_token", c.cfg.RefreshToken)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.oauthURL, strings.NewReader(values.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("refresh google ads access token failed: status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var tokenResp tokenResponse
	if err := json.Unmarshal(raw, &tokenResp); err != nil {
		return "", err
	}
	if tokenResp.AccessToken == "" {
		return "", fmt.Errorf("refresh google ads access token returned empty token")
	}
	c.token = tokenResp.AccessToken
	c.expiresAt = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	return c.token, nil
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
}

type searchRequest struct {
	Query string `json:"query"`
}

type searchResponse struct {
	Results []searchResult `json:"results"`
}

type searchResult struct {
	CampaignCriterion campaignCriterionRow `json:"campaignCriterion"`
	AdGroup           adGroupRow           `json:"adGroup"`
}

type campaignCriterionRow struct {
	ResourceName string           `json:"resourceName"`
	Proximity    proximityInfoRow `json:"proximity"`
}

type proximityInfoRow struct {
	GeoPoint    geoPointInfo `json:"geoPoint"`
	Radius      float64      `json:"radius"`
	RadiusUnits string       `json:"radiusUnits"`
}

type adGroupRow struct {
	ResourceName string `json:"resourceName"`
	Name         string `json:"name"`
}

type mutateCampaignCriteriaRequest struct {
	Operations []campaignCriterionOperation `json:"operations"`
}

type campaignCriterionOperation struct {
	Create *campaignCriterion `json:"create,omitempty"`
	Remove string             `json:"remove,omitempty"`
}

type campaignCriterion struct {
	Campaign  string         `json:"campaign"`
	Proximity *proximityInfo `json:"proximity,omitempty"`
}

type proximityInfo struct {
	GeoPoint    *geoPointInfo `json:"geoPoint,omitempty"`
	Radius      float64       `json:"radius"`
	RadiusUnits string        `json:"radiusUnits"`
}

type geoPointInfo struct {
	LatitudeInMicroDegrees  microDegrees `json:"latitudeInMicroDegrees"`
	LongitudeInMicroDegrees microDegrees `json:"longitudeInMicroDegrees"`
}

type microDegrees int64

func (m *microDegrees) UnmarshalJSON(raw []byte) error {
	var number int64
	if err := json.Unmarshal(raw, &number); err == nil {
		*m = microDegrees(number)
		return nil
	}

	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return err
	}
	parsed, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return err
	}
	*m = microDegrees(parsed)
	return nil
}

func (m microDegrees) MarshalJSON() ([]byte, error) {
	return []byte(strconv.FormatInt(int64(m), 10)), nil
}

type mutateCampaignCriteriaResponse struct {
	Results []mutateResult `json:"results"`
}

type mutateResult struct {
	ResourceName string `json:"resourceName"`
}

type mutateResponse struct {
	Results []mutateResult `json:"results"`
}

type mutateAdGroupsRequest struct {
	Operations []adGroupOperation `json:"operations"`
}

type adGroupOperation struct {
	Create *adGroup `json:"create,omitempty"`
	Remove string   `json:"remove,omitempty"`
}

type adGroup struct {
	Campaign string `json:"campaign"`
	Name     string `json:"name"`
	Status   string `json:"status"`
	Type     string `json:"type"`
}

type mutateAdGroupCriteriaRequest struct {
	Operations []adGroupCriterionOperation `json:"operations"`
}

type adGroupCriterionOperation struct {
	Create *adGroupCriterion `json:"create,omitempty"`
	Remove string            `json:"remove,omitempty"`
}

type adGroupCriterion struct {
	AdGroup   string         `json:"adGroup"`
	Status    string         `json:"status,omitempty"`
	Proximity *proximityInfo `json:"proximity,omitempty"`
}

type StoreAd struct {
	AdGroup      string
	FinalURL     string
	Status       string
	Headlines    []string
	Descriptions []string
}

type mutateAdGroupAdsRequest struct {
	Operations []adGroupAdOperation `json:"operations"`
}

type adGroupAdOperation struct {
	Create *adGroupAd `json:"create,omitempty"`
	Remove string     `json:"remove,omitempty"`
}

type adGroupAd struct {
	AdGroup string                    `json:"adGroup"`
	Status  string                    `json:"status"`
	Ad      responsiveSearchAdWrapper `json:"ad"`
}

type responsiveSearchAdWrapper struct {
	FinalURLs          []string           `json:"finalUrls"`
	ResponsiveSearchAd responsiveSearchAd `json:"responsiveSearchAd"`
}

type responsiveSearchAd struct {
	Headlines    []adTextAsset `json:"headlines"`
	Descriptions []adTextAsset `json:"descriptions"`
}

type adTextAsset struct {
	Text string `json:"text"`
}

func adTextAssets(texts []string) []adTextAsset {
	assets := make([]adTextAsset, 0, len(texts))
	for _, text := range texts {
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		assets = append(assets, adTextAsset{Text: text})
	}
	return assets
}

func mutationResourceNames(response mutateResponse) []string {
	resourceNames := make([]string, 0, len(response.Results))
	for _, result := range response.Results {
		resourceNames = append(resourceNames, result.ResourceName)
	}
	return resourceNames
}

func AdGroupName(target Target) string {
	name := strings.TrimSpace(target.StoreName)
	if name == "" {
		name = "Store"
	}
	return fmt.Sprintf("Careme Store %s %s", target.StoreID, name)
}
