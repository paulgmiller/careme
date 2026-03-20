package safewayads

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	safewayBanner               = "safeway"
	safewayBaseURL              = "https://www.safeway.com"
	storeDetailsSubscriptionKey = "b2ea4df305624d96960191e1aaed9b9d"
	safewayMerchantName         = "safeway"
	safewayMerchantAccessToken  = "7749fa974b9869e8f57606ac9477decf"
	flippPublicationURL         = "https://api.flipp.com/flyerkit/v4.0/publications/"
	defaultImageContentType     = "application/octet-stream"
)

type Client struct {
	httpClient *http.Client
}

type FetchResult struct {
	StoreID       string
	StoreCode     string
	StoreName     string
	City          string
	State         string
	PostalCode    string
	SourcePageURL string
	Publication   Publication
	PDFURL        string
	ImageURL      string
	ImageBytes    []byte
	ContentType   string
	Checksum      string
	Pages         []FetchedPage
}

type FetchedPage struct {
	PageNumber  int
	ImageURL    string
	ImageBytes  []byte
	ContentType string
	Checksum    string
}

func NewClient(httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{httpClient: httpClient}
}

func FormatStoreID(id int) string {
	if id < 0 {
		id = 0
	}
	return fmt.Sprintf("%03d", id)
}

func CanonicalStoreCode(storeID string) string {
	storeID = strings.TrimSpace(storeID)
	if storeID == "" {
		return ""
	}
	n, err := strconv.Atoi(storeID)
	if err != nil {
		return storeID
	}
	return strconv.Itoa(n)
}

func SourcePageURL(storeID string) string {
	return fmt.Sprintf("%s/set-store.html?storeId=%s&target=weeklyad", safewayBaseURL, CanonicalStoreCode(storeID))
}

func ImageKey(storeID string, publicationID int64, imageURL, contentType string, imageBytes []byte, now time.Time) string {
	return PageImageKey(storeID, publicationID, 1, imageURL, contentType, imageBytes, now)
}

func PageImageKey(storeID string, publicationID int64, pageNumber int, imageURL, contentType string, imageBytes []byte, now time.Time) string {
	ext := fileExtension(imageURL, contentType, imageBytes)
	return fmt.Sprintf("safeway/weeklyads/images/%s/%s_%d_p%02d%s", storeID, now.UTC().Format("2006-01-02"), publicationID, pageNumber, ext)
}

func IngredientsKey(storeID string, publicationID int64, now time.Time) string {
	return fmt.Sprintf("safeway/weeklyads/ingredients/%s/%s_%d.json", storeID, now.UTC().Format("2006-01-02"), publicationID)
}

func StatusKey(storeID string) string {
	return fmt.Sprintf("safeway/weeklyads/runs/%s.json", storeID)
}

func (c *Client) FetchWeeklyAdAssets(ctx context.Context, storeID string) (*FetchResult, error) {
	storeCode := CanonicalStoreCode(storeID)
	details, err := c.getStoreDetails(ctx, storeCode)
	if err != nil {
		return nil, err
	}
	postalCode := strings.TrimSpace(details.Store.Address.ZipCode)
	if postalCode == "" {
		return nil, fmt.Errorf("store %s missing zipcode", storeCode)
	}

	publications, err := c.getPublications(ctx, storeCode, postalCode)
	if err != nil {
		return nil, err
	}
	publication, ok := SelectWeeklyAd(publications)
	if !ok {
		return nil, ErrNoAd
	}

	result := &FetchResult{
		StoreID:       storeID,
		StoreCode:     storeCode,
		StoreName:     details.Store.DomainName,
		City:          details.Store.Address.City,
		State:         details.Store.Address.State,
		PostalCode:    postalCode,
		SourcePageURL: SourcePageURL(storeID),
		Publication:   publication,
		PDFURL:        strings.TrimSpace(publication.PDFURL),
		ImageURL:      FirstPageImageURL(publication),
	}

	if result.PDFURL != "" {
		pdfBytes, _, err := c.downloadBytes(ctx, result.PDFURL)
		if err == nil {
			pages, renderErr := renderPDFPages(ctx, pdfBytes)
			if renderErr == nil && len(pages) > 0 {
				result.Pages = pages
				result.ImageBytes = pages[0].ImageBytes
				result.ContentType = pages[0].ContentType
				result.Checksum = pages[0].Checksum
				return result, nil
			}
		}
	}

	if result.ImageURL == "" {
		return nil, ErrNoAd
	}
	imageBytes, contentType, err := c.downloadBytes(ctx, result.ImageURL)
	if err != nil {
		return nil, err
	}
	page := FetchedPage{
		PageNumber:  1,
		ImageURL:    result.ImageURL,
		ImageBytes:  imageBytes,
		ContentType: contentType,
		Checksum:    checksum(imageBytes),
	}
	result.ImageBytes = page.ImageBytes
	result.ContentType = page.ContentType
	result.Checksum = page.Checksum
	result.Pages = []FetchedPage{page}
	return result, nil
}

func SelectWeeklyAd(publications []Publication) (Publication, bool) {
	for _, publication := range publications {
		if strings.EqualFold(strings.TrimSpace(publication.ExternalDisplayName), "Weekly Ad") {
			return publication, true
		}
	}
	if len(publications) == 0 {
		return Publication{}, false
	}
	return publications[0], true
}

func FirstPageImageURL(publication Publication) string {
	for _, value := range []string{
		publication.FirstPageThumbnail2000URL,
		publication.FirstPageThumbnailURL,
		publication.ImageFirstPage400W,
		publication.ThumbnailImageURL,
	} {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func (c *Client) getStoreDetails(ctx context.Context, storeCode string) (*StoreDetailsResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, safewayBaseURL+"/api/services/locator/entity/store/"+storeCode, nil)
	if err != nil {
		return nil, fmt.Errorf("build store request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Ocp-Apim-Subscription-Key", storeDetailsSubscriptionKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request store details: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("store details status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var details StoreDetailsResponse
	if err := json.NewDecoder(resp.Body).Decode(&details); err != nil {
		return nil, fmt.Errorf("decode store details: %w", err)
	}
	return &details, nil
}

func (c *Client) getPublications(ctx context.Context, storeCode, postalCode string) ([]Publication, error) {
	u, err := url.Parse(flippPublicationURL + safewayMerchantName)
	if err != nil {
		return nil, fmt.Errorf("build publication url: %w", err)
	}
	q := u.Query()
	q.Set("access_token", safewayMerchantAccessToken)
	q.Set("locale", "en-US")
	q.Set("store_code", storeCode)
	q.Set("postal_code", postalCode)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("build publication request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request publications: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		if isInvalidStoreResponse(resp.StatusCode, body) {
			return nil, ErrInvalidStore
		}
		return nil, fmt.Errorf("publication status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var publications []Publication
	if err := json.NewDecoder(resp.Body).Decode(&publications); err != nil {
		return nil, fmt.Errorf("decode publications: %w", err)
	}
	return publications, nil
}

func isInvalidStoreResponse(statusCode int, body []byte) bool {
	if statusCode != http.StatusUnprocessableEntity {
		return false
	}
	return strings.Contains(strings.ToLower(string(body)), "invalid store_code")
}

func (c *Client) downloadBytes(ctx context.Context, assetURL string) ([]byte, string, error) {
	assetURL = strings.TrimSpace(assetURL)
	if assetURL == "" {
		return nil, "", fmt.Errorf("asset url is required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, assetURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("build asset request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("request asset: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, "", fmt.Errorf("asset status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	contentType := strings.TrimSpace(resp.Header.Get("Content-Type"))
	if contentType == "" {
		contentType = defaultImageContentType
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("read asset: %w", err)
	}
	return data, contentType, nil
}

func renderPDFPages(ctx context.Context, pdfBytes []byte) ([]FetchedPage, error) {
	if len(pdfBytes) == 0 {
		return nil, fmt.Errorf("pdf bytes are required")
	}
	dir, err := os.MkdirTemp("", "safeway-weeklyad-*")
	if err != nil {
		return nil, fmt.Errorf("make temp dir: %w", err)
	}
	defer func() {
		_ = os.RemoveAll(dir)
	}()

	inputPath := filepath.Join(dir, "weeklyad.pdf")
	if err := os.WriteFile(inputPath, pdfBytes, 0600); err != nil {
		return nil, fmt.Errorf("write temp pdf: %w", err)
	}
	outputPrefix := filepath.Join(dir, "page")
	cmd := exec.CommandContext(ctx, "pdftoppm", "-jpeg", "-jpegopt", "quality=90", "-r", "150", inputPath, outputPrefix)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("render pdf pages: %w: %s", err, strings.TrimSpace(string(output)))
	}

	paths, err := filepath.Glob(outputPrefix + "-*.jpg")
	if err != nil {
		return nil, fmt.Errorf("find rendered pages: %w", err)
	}
	sort.Slice(paths, func(i, j int) bool {
		return pageNumberFromPath(paths[i]) < pageNumberFromPath(paths[j])
	})

	pages := make([]FetchedPage, 0, len(paths))
	for _, pagePath := range paths {
		pageNumber := pageNumberFromPath(pagePath)
		if pageNumber <= 0 {
			continue
		}
		data, err := os.ReadFile(pagePath)
		if err != nil {
			return nil, fmt.Errorf("read rendered page %d: %w", pageNumber, err)
		}
		pages = append(pages, FetchedPage{
			PageNumber:  pageNumber,
			ImageBytes:  data,
			ContentType: "image/jpeg",
			Checksum:    checksum(data),
		})
	}
	if len(pages) == 0 {
		return nil, fmt.Errorf("pdf render produced no pages")
	}
	return pages, nil
}

func pageNumberFromPath(pagePath string) int {
	base := strings.TrimSuffix(filepath.Base(pagePath), filepath.Ext(pagePath))
	idx := strings.LastIndex(base, "-")
	if idx < 0 || idx+1 >= len(base) {
		return 0
	}
	n, err := strconv.Atoi(base[idx+1:])
	if err != nil {
		return 0
	}
	return n
}

func checksum(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func fileExtension(rawURL, contentType string, imageBytes []byte) string {
	for _, candidate := range []string{contentType, http.DetectContentType(imageBytes)} {
		switch strings.ToLower(strings.TrimSpace(candidate)) {
		case "image/jpeg":
			return ".jpg"
		case "image/png":
			return ".png"
		case "image/gif":
			return ".gif"
		case "image/webp":
			return ".webp"
		}
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return ".bin"
	}
	ext := strings.ToLower(path.Ext(u.Path))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp":
		return ext
	default:
		return ".bin"
	}
}

var ErrNoAd = fmt.Errorf("no weekly ad publication found")
var ErrInvalidStore = fmt.Errorf("invalid store code")
