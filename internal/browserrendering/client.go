package browserrendering

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const DefaultBaseURL = "https://api.cloudflare.com/client/v4/accounts"

type Client struct {
	baseURL    string
	accountID  string
	apiToken   string
	httpClient *http.Client
}

type GotoOptions struct {
	Referer   string `json:"referer,omitempty"`
	Referrer  string `json:"referrer,omitempty"`
	Timeout   int    `json:"timeout,omitempty"`
	WaitUntil string `json:"waitUntil,omitempty"`
}

type WaitForSelector struct {
	Selector string `json:"selector"`
	Timeout  int    `json:"timeout,omitempty"`
	Visible  bool   `json:"visible,omitempty"`
	Hidden   bool   `json:"hidden,omitempty"`
}

type Authenticate struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type Cookie struct {
	Name     string `json:"name"`
	Value    string `json:"value"`
	Domain   string `json:"domain,omitempty"`
	Path     string `json:"path,omitempty"`
	URL      string `json:"url,omitempty"`
	Expires  int64  `json:"expires,omitempty"`
	HTTPOnly bool   `json:"httpOnly,omitempty"`
	Secure   bool   `json:"secure,omitempty"`
}

type ResponseFormat struct {
	Type       string `json:"type"`
	Schema     any    `json:"schema,omitempty"`
	JSONSchema any    `json:"json_schema,omitempty"`
}

type CustomAI struct {
	Model         string `json:"model"`
	Authorization string `json:"authorization"`
}

type JSONRequest struct {
	URL                  string            `json:"url,omitempty"`
	HTML                 string            `json:"html,omitempty"`
	Prompt               string            `json:"prompt,omitempty"`
	ResponseFormat       *ResponseFormat   `json:"response_format,omitempty"`
	CustomAI             []CustomAI        `json:"custom_ai,omitempty"`
	Authenticate         *Authenticate     `json:"authenticate,omitempty"`
	Cookies              []Cookie          `json:"cookies,omitempty"`
	GotoOptions          *GotoOptions      `json:"gotoOptions,omitempty"`
	WaitForSelector      *WaitForSelector  `json:"waitForSelector,omitempty"`
	UserAgent            string            `json:"userAgent,omitempty"`
	SetExtraHTTPHeaders  map[string]string `json:"setExtraHTTPHeaders,omitempty"`
	RejectResourceTypes  []string          `json:"rejectResourceTypes,omitempty"`
	RejectRequestPattern []string          `json:"rejectRequestPattern,omitempty"`
	AllowRequestPattern  []string          `json:"allowRequestPattern,omitempty"`
}

type ContentRequest struct {
	URL                  string            `json:"url,omitempty"`
	HTML                 string            `json:"html,omitempty"`
	Authenticate         *Authenticate     `json:"authenticate,omitempty"`
	Cookies              []Cookie          `json:"cookies,omitempty"`
	GotoOptions          *GotoOptions      `json:"gotoOptions,omitempty"`
	WaitForSelector      *WaitForSelector  `json:"waitForSelector,omitempty"`
	UserAgent            string            `json:"userAgent,omitempty"`
	SetExtraHTTPHeaders  map[string]string `json:"setExtraHTTPHeaders,omitempty"`
	RejectResourceTypes  []string          `json:"rejectResourceTypes,omitempty"`
	RejectRequestPattern []string          `json:"rejectRequestPattern,omitempty"`
	AllowRequestPattern  []string          `json:"allowRequestPattern,omitempty"`
}

type ScrapeElement struct {
	Selector string `json:"selector"`
}

type ScrapeRequest struct {
	URL                 string            `json:"url,omitempty"`
	Elements            []ScrapeElement   `json:"elements,omitempty"`
	Authenticate        *Authenticate     `json:"authenticate,omitempty"`
	Cookies             []Cookie          `json:"cookies,omitempty"`
	GotoOptions         *GotoOptions      `json:"gotoOptions,omitempty"`
	WaitForSelector     *WaitForSelector  `json:"waitForSelector,omitempty"`
	UserAgent           string            `json:"userAgent,omitempty"`
	SetExtraHTTPHeaders map[string]string `json:"setExtraHTTPHeaders,omitempty"`
}

type JSONOptions struct {
	Prompt         string          `json:"prompt,omitempty"`
	ResponseFormat *ResponseFormat `json:"response_format,omitempty"`
	CustomAI       []CustomAI      `json:"custom_ai,omitempty"`
}

type CrawlOptions struct {
	IncludeExternalLinks *bool    `json:"includeExternalLinks,omitempty"`
	IncludeSubdomains    *bool    `json:"includeSubdomains,omitempty"`
	IncludePatterns      []string `json:"includePatterns,omitempty"`
	ExcludePatterns      []string `json:"excludePatterns,omitempty"`
}

type CrawlRequest struct {
	URL                  string            `json:"url"`
	Limit                int               `json:"limit,omitempty"`
	Depth                int               `json:"depth,omitempty"`
	Source               string            `json:"source,omitempty"`
	Formats              []string          `json:"formats,omitempty"`
	Render               *bool             `json:"render,omitempty"`
	JSONOptions          *JSONOptions      `json:"jsonOptions,omitempty"`
	MaxAge               int               `json:"maxAge,omitempty"`
	ModifiedSince        int64             `json:"modifiedSince,omitempty"`
	Authenticate         *Authenticate     `json:"authenticate,omitempty"`
	Cookies              []Cookie          `json:"cookies,omitempty"`
	GotoOptions          *GotoOptions      `json:"gotoOptions,omitempty"`
	WaitForSelector      *WaitForSelector  `json:"waitForSelector,omitempty"`
	SetExtraHTTPHeaders  map[string]string `json:"setExtraHTTPHeaders,omitempty"`
	RejectResourceTypes  []string          `json:"rejectResourceTypes,omitempty"`
	RejectRequestPattern []string          `json:"rejectRequestPattern,omitempty"`
	AllowRequestPattern  []string          `json:"allowRequestPattern,omitempty"`
	Options              *CrawlOptions     `json:"options,omitempty"`
}

type ScrapeAttribute struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type ScrapeMatch struct {
	Text       string            `json:"text"`
	HTML       string            `json:"html"`
	Attributes []ScrapeAttribute `json:"attributes,omitempty"`
	Height     float64           `json:"height,omitempty"`
	Width      float64           `json:"width,omitempty"`
	Top        float64           `json:"top,omitempty"`
	Left       float64           `json:"left,omitempty"`
}

type ScrapeResult struct {
	Selector string        `json:"selector"`
	Results  []ScrapeMatch `json:"results"`
}

type CrawlMetadata struct {
	Status int    `json:"status,omitempty"`
	Title  string `json:"title,omitempty"`
	URL    string `json:"url,omitempty"`
}

type CrawlRecord struct {
	URL      string          `json:"url"`
	Status   string          `json:"status"`
	HTML     string          `json:"html,omitempty"`
	Markdown string          `json:"markdown,omitempty"`
	JSON     json.RawMessage `json:"json,omitempty"`
	Metadata CrawlMetadata   `json:"metadata,omitempty"`
}

type CrawlJob struct {
	ID                 string        `json:"id"`
	Status             string        `json:"status"`
	BrowserSecondsUsed float64       `json:"browserSecondsUsed,omitempty"`
	Total              int           `json:"total,omitempty"`
	Finished           int           `json:"finished,omitempty"`
	Records            []CrawlRecord `json:"records,omitempty"`
	Cursor             any           `json:"cursor,omitempty"`
}

type CrawlResultsQuery struct {
	Cursor string
	Limit  int
	Status string
}

type CrawlWaitOptions struct {
	MaxAttempts int
	Delay       time.Duration
	PollLimit   int
}

type apiEnvelope[T any] struct {
	Success bool       `json:"success"`
	Result  T          `json:"result"`
	Errors  []apiError `json:"errors,omitempty"`
}

type apiError struct {
	Code    any    `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

func NewClient(accountID, apiToken string, httpClient *http.Client) (*Client, error) {
	return NewClientWithBaseURL(DefaultBaseURL, accountID, apiToken, httpClient)
}

func NewClientWithBaseURL(baseURL, accountID, apiToken string, httpClient *http.Client) (*Client, error) {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return nil, errors.New("cloudflare account id is required")
	}
	apiToken = strings.TrimSpace(apiToken)
	if apiToken == "" {
		return nil, errors.New("cloudflare api token is required")
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 90 * time.Second}
	}
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		accountID:  accountID,
		apiToken:   apiToken,
		httpClient: httpClient,
	}, nil
}

func (c *Client) Content(ctx context.Context, payload ContentRequest) ([]byte, error) {
	body, err := c.do(ctx, http.MethodPost, "browser-rendering/content", nil, payload)
	if err != nil {
		return nil, err
	}
	return body, nil
}

func (c *Client) Scrape(ctx context.Context, payload ScrapeRequest) ([]ScrapeResult, error) {
	var envelope apiEnvelope[[]ScrapeResult]
	if err := c.postJSON(ctx, "browser-rendering/scrape", payload, &envelope); err != nil {
		return nil, err
	}
	return envelope.Result, nil
}

func (c *Client) JSON(ctx context.Context, payload JSONRequest) (json.RawMessage, error) {
	var envelope apiEnvelope[json.RawMessage]
	if err := c.postJSON(ctx, "browser-rendering/json", payload, &envelope); err != nil {
		return nil, err
	}
	return envelope.Result, nil
}

func (c *Client) JSONInto(ctx context.Context, payload JSONRequest, dest any) error {
	raw, err := c.JSON(ctx, payload)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, dest); err != nil {
		return fmt.Errorf("decode json endpoint result: %w", err)
	}
	return nil
}

func (c *Client) StartCrawl(ctx context.Context, payload CrawlRequest) (string, error) {
	var envelope apiEnvelope[string]
	if err := c.postJSON(ctx, "browser-rendering/crawl", payload, &envelope); err != nil {
		return "", err
	}
	if strings.TrimSpace(envelope.Result) == "" {
		return "", errors.New("crawl start response missing job id")
	}
	return envelope.Result, nil
}

func (c *Client) GetCrawl(ctx context.Context, jobID string, query CrawlResultsQuery) (*CrawlJob, error) {
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return nil, errors.New("crawl job id is required")
	}

	values := make(url.Values)
	if query.Cursor != "" {
		values.Set("cursor", query.Cursor)
	}
	if query.Limit > 0 {
		values.Set("limit", strconv.Itoa(query.Limit))
	}
	if query.Status != "" {
		values.Set("status", query.Status)
	}

	var envelope apiEnvelope[CrawlJob]
	if err := c.getJSON(ctx, "browser-rendering/crawl/"+url.PathEscape(jobID), values, &envelope); err != nil {
		return nil, err
	}
	return &envelope.Result, nil
}

func (c *Client) CancelCrawl(ctx context.Context, jobID string) error {
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return errors.New("crawl job id is required")
	}
	_, err := c.do(ctx, http.MethodDelete, "browser-rendering/crawl/"+url.PathEscape(jobID), nil, nil)
	return err
}

func (c *Client) GetCrawlAll(ctx context.Context, jobID string) (*CrawlJob, error) {
	job, err := c.GetCrawl(ctx, jobID, CrawlResultsQuery{})
	if err != nil {
		return nil, err
	}

	allRecords := append([]CrawlRecord(nil), job.Records...)
	cursor := crawlCursorString(job.Cursor)
	for cursor != "" {
		page, err := c.GetCrawl(ctx, jobID, CrawlResultsQuery{Cursor: cursor})
		if err != nil {
			return nil, err
		}
		allRecords = append(allRecords, page.Records...)
		cursor = crawlCursorString(page.Cursor)
	}

	job.Records = allRecords
	job.Cursor = nil
	return job, nil
}

func (c *Client) WaitForCrawl(ctx context.Context, jobID string, opts CrawlWaitOptions) (*CrawlJob, error) {
	maxAttempts := opts.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 60
	}
	delay := opts.Delay
	if delay <= 0 {
		delay = 5 * time.Second
	}
	pollLimit := opts.PollLimit
	if pollLimit <= 0 {
		pollLimit = 1
	}

	for attempt := 0; attempt < maxAttempts; attempt++ {
		job, err := c.GetCrawl(ctx, jobID, CrawlResultsQuery{Limit: pollLimit})
		if err != nil {
			return nil, err
		}
		if job.Status != "running" {
			return job, nil
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}

	return nil, fmt.Errorf("crawl job %q did not complete after %d attempts", jobID, maxAttempts)
}

func (c *Client) postJSON(ctx context.Context, path string, payload any, dest any) error {
	body, err := c.do(ctx, http.MethodPost, path, nil, payload)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, dest); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return validateEnvelope(dest)
}

func (c *Client) getJSON(ctx context.Context, path string, query url.Values, dest any) error {
	body, err := c.do(ctx, http.MethodGet, path, query, nil)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, dest); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return validateEnvelope(dest)
}

func (c *Client) do(ctx context.Context, method, path string, query url.Values, payload any) ([]byte, error) {
	endpoint := c.baseURL + "/" + c.accountID + "/" + strings.TrimLeft(path, "/")
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}

	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("marshal request body: %w", err)
		}
		body = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiToken)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s %q: %w", method, endpoint, err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, formatAPIError(resp.StatusCode, respBody)
	}
	return respBody, nil
}

func validateEnvelope(dest any) error {
	raw, err := json.Marshal(dest)
	if err != nil {
		return nil
	}

	var envelope apiEnvelope[json.RawMessage]
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil
	}
	if envelope.Success {
		return nil
	}
	if len(envelope.Errors) == 0 {
		return errors.New("browser rendering request returned success=false")
	}
	return formatStructuredErrors(envelope.Errors)
}

func formatAPIError(status int, body []byte) error {
	var envelope apiEnvelope[json.RawMessage]
	if err := json.Unmarshal(body, &envelope); err == nil && len(envelope.Errors) > 0 {
		return fmt.Errorf("browser rendering request failed: status %d: %w", status, formatStructuredErrors(envelope.Errors))
	}

	message := strings.TrimSpace(string(body))
	if message == "" {
		message = http.StatusText(status)
	}
	return fmt.Errorf("browser rendering request failed: status %d: %s", status, message)
}

func formatStructuredErrors(errs []apiError) error {
	parts := make([]string, 0, len(errs))
	for _, apiErr := range errs {
		message := strings.TrimSpace(apiErr.Message)
		if message == "" {
			message = "unknown error"
		}
		if apiErr.Code != nil {
			message = fmt.Sprintf("%v: %s", apiErr.Code, message)
		}
		parts = append(parts, message)
	}
	return errors.New(strings.Join(parts, "; "))
}

func crawlCursorString(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(v)
	default:
		cursor := strings.TrimSpace(fmt.Sprint(v))
		if cursor == "<nil>" {
			return ""
		}
		return cursor
	}
}
