package brightdata

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

const (
	DefaultBaseURL      = "https://api.brightdata.com"
	defaultPollInterval = 2 * time.Second
)

type Format string

const (
	FormatJSON   Format = "json"
	FormatNDJSON Format = "ndjson"
	FormatCSV    Format = "csv"
)

type SnapshotStatus string

const (
	SnapshotStatusStarting SnapshotStatus = "starting"
	SnapshotStatusRunning  SnapshotStatus = "running"
	SnapshotStatusBuilding SnapshotStatus = "building"
	SnapshotStatusReady    SnapshotStatus = "ready"
	SnapshotStatusFailed   SnapshotStatus = "failed"
)

// Client calls the Bright Data scraper API.
type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

// APIError captures a non-success Bright Data response.
type APIError struct {
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	body := strings.TrimSpace(e.Body)
	if body == "" {
		return fmt.Sprintf("bright data request failed: status %d", e.StatusCode)
	}
	return fmt.Sprintf("bright data request failed: status %d: %s", e.StatusCode, body)
}

type ScrapeOptions struct {
	Notify             bool
	IncludeErrors      bool
	Format             Format
	CustomOutputFields string
}

type TriggerOptions struct {
	Notify             bool
	IncludeErrors      bool
	Format             Format
	CustomOutputFields string
}

type DownloadOptions struct {
	Format Format
}

type ScrapeResponse struct {
	StatusCode  int
	ContentType string
	Header      http.Header
	Body        []byte
	SnapshotID  string
	Message     string
}

func (r *ScrapeResponse) DecodeJSON(dest any) error {
	if len(r.Body) == 0 {
		return errors.New("response body is empty")
	}
	if err := json.Unmarshal(r.Body, dest); err != nil {
		return fmt.Errorf("decode json response: %w", err)
	}
	return nil
}

type TriggerResponse struct {
	SnapshotID string `json:"snapshot_id"`
}

type ProgressResponse struct {
	SnapshotID   string         `json:"snapshot_id"`
	DatasetID    string         `json:"dataset_id"`
	Status       SnapshotStatus `json:"status"`
	ErrorMessage string         `json:"error_message,omitempty"`
}

func (r *ProgressResponse) Ready() bool {
	return r != nil && r.Status == SnapshotStatusReady
}

func (r *ProgressResponse) Failed() bool {
	return r != nil && r.Status == SnapshotStatusFailed
}

type DownloadResponse struct {
	StatusCode  int
	ContentType string
	Header      http.Header
	Body        []byte
	Ready       bool
	Status      SnapshotStatus
	Message     string
}

func (r *DownloadResponse) DecodeJSON(dest any) error {
	if len(r.Body) == 0 {
		return errors.New("response body is empty")
	}
	if err := json.Unmarshal(r.Body, dest); err != nil {
		return fmt.Errorf("decode json response: %w", err)
	}
	return nil
}

type DeliveryExtension string

const (
	DeliveryExtensionJSON   DeliveryExtension = "json"
	DeliveryExtensionNDJSON DeliveryExtension = "ndjson"
	DeliveryExtensionCSV    DeliveryExtension = "csv"
)

type AzureDeliveryOptions struct {
	Container        string
	Account          string
	Key              string
	SASToken         string
	Directory        string
	FilenameTemplate string
	Extension        DeliveryExtension
	Compress         bool
	BatchSize        int
}

type DeliveryResponse struct {
	DeliveryID string `json:"delivery_id"`
}

type DeliveryStatus struct {
	DeliveryID string `json:"delivery_id"`
	Status     string `json:"status"`
	Message    string `json:"message,omitempty"`
}

type asyncResponse struct {
	SnapshotID string         `json:"snapshot_id"`
	Status     SnapshotStatus `json:"status,omitempty"`
	Message    string         `json:"message,omitempty"`
}

type scrapePayload struct {
	Input              any    `json:"input"`
	CustomOutputFields string `json:"custom_output_fields,omitempty"`
}

type deliverSnapshotPayload struct {
	Deliver   azureDeliveryPayload `json:"deliver"`
	Compress  bool                 `json:"compress"`
	BatchSize int                  `json:"batch_size,omitempty"`
}

type azureDeliveryPayload struct {
	Type        string                  `json:"type"`
	Filename    azureFilenamePayload    `json:"filename"`
	Container   string                  `json:"container"`
	Credentials azureCredentialsPayload `json:"credentials"`
	Directory   string                  `json:"directory,omitempty"`
}

type azureFilenamePayload struct {
	Template  string `json:"template"`
	Extension string `json:"extension"`
}

type azureCredentialsPayload struct {
	Account  string `json:"account"`
	Key      string `json:"key,omitempty"`
	SASToken string `json:"sas_token,omitempty"`
}

// NewClient creates a Bright Data client with the default API base URL.
func NewClient(apiKey string, httpClient *http.Client) (*Client, error) {
	return NewClientWithBaseURL(DefaultBaseURL, apiKey, httpClient)
}

// NewClientWithBaseURL creates a Bright Data client for the provided base URL.
func NewClientWithBaseURL(baseURL, apiKey string, httpClient *http.Client) (*Client, error) {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, errors.New("api key is required")
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}

	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		apiKey:     apiKey,
		httpClient: httpClient,
	}, nil
}

// Scrape sends a synchronous scrape request. When Bright Data cannot finish
// within the sync timeout, it returns a snapshot ID that can be polled later.
func (c *Client) Scrape(ctx context.Context, datasetID string, input any, opts ScrapeOptions) (*ScrapeResponse, error) {
	if err := validateDatasetAndInput(datasetID, input); err != nil {
		return nil, err
	}

	req, err := c.newJSONRequest(ctx, http.MethodPost, "/datasets/v3/scrape", queryForRequest(datasetID, opts.Notify, opts.IncludeErrors, opts.Format, opts.CustomOutputFields), scrapePayload{
		Input:              input,
		CustomOutputFields: strings.TrimSpace(opts.CustomOutputFields),
	})
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("scrape request: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read scrape response: %w", err)
	}

	if resp.StatusCode == http.StatusAccepted {
		var async asyncResponse
		if err := json.Unmarshal(body, &async); err != nil {
			return nil, fmt.Errorf("decode async scrape response: %w", err)
		}
		return &ScrapeResponse{
			StatusCode:  resp.StatusCode,
			ContentType: resp.Header.Get("Content-Type"),
			Header:      resp.Header.Clone(),
			Body:        body,
			SnapshotID:  async.SnapshotID,
			Message:     async.Message,
		}, nil
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, &APIError{StatusCode: resp.StatusCode, Body: string(body)}
	}

	return &ScrapeResponse{
		StatusCode:  resp.StatusCode,
		ContentType: resp.Header.Get("Content-Type"),
		Header:      resp.Header.Clone(),
		Body:        body,
	}, nil
}

// Trigger starts an asynchronous scrape and returns the snapshot ID.
func (c *Client) Trigger(ctx context.Context, datasetID string, input any, opts TriggerOptions) (*TriggerResponse, error) {
	if err := validateDatasetAndInput(datasetID, input); err != nil {
		return nil, err
	}

	req, err := c.newJSONRequest(ctx, http.MethodPost, "/datasets/v3/trigger", queryForRequest(datasetID, opts.Notify, opts.IncludeErrors, opts.Format, opts.CustomOutputFields), input)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("trigger request: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read trigger response: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, &APIError{StatusCode: resp.StatusCode, Body: string(body)}
	}

	var out TriggerResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("decode trigger response: %w", err)
	}
	if strings.TrimSpace(out.SnapshotID) == "" {
		return nil, errors.New("trigger response missing snapshot_id")
	}
	return &out, nil
}

// Progress fetches the status of an asynchronous snapshot.
func (c *Client) Progress(ctx context.Context, snapshotID string) (*ProgressResponse, error) {
	snapshotID = strings.TrimSpace(snapshotID)
	if snapshotID == "" {
		return nil, errors.New("snapshot ID is required")
	}

	req, err := c.newJSONRequest(ctx, http.MethodGet, "/datasets/v3/progress/"+url.PathEscape(snapshotID), nil, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("progress request: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read progress response: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, &APIError{StatusCode: resp.StatusCode, Body: string(body)}
	}

	var out ProgressResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("decode progress response: %w", err)
	}
	return &out, nil
}

// WaitForSnapshot polls progress until the snapshot is ready or failed.
func (c *Client) WaitForSnapshot(ctx context.Context, snapshotID string, pollInterval time.Duration) (*ProgressResponse, error) {
	if pollInterval <= 0 {
		pollInterval = defaultPollInterval
	}

	for {
		progress, err := c.Progress(ctx, snapshotID)
		if err != nil {
			return nil, err
		}
		if progress.Ready() {
			return progress, nil
		}
		if progress.Failed() {
			if progress.ErrorMessage == "" {
				return progress, fmt.Errorf("snapshot %s failed", snapshotID)
			}
			return progress, fmt.Errorf("snapshot %s failed: %s", snapshotID, progress.ErrorMessage)
		}

		timer := time.NewTimer(pollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

// DownloadSnapshot retrieves a completed snapshot body. If the snapshot is not
// ready yet, Bright Data responds with HTTP 202 and a status message.
func (c *Client) DownloadSnapshot(ctx context.Context, snapshotID string, opts DownloadOptions) (*DownloadResponse, error) {
	snapshotID = strings.TrimSpace(snapshotID)
	if snapshotID == "" {
		return nil, errors.New("snapshot ID is required")
	}

	query := url.Values{}
	if opts.Format != "" {
		query.Set("format", string(opts.Format))
	}

	req, err := c.newJSONRequest(ctx, http.MethodGet, "/datasets/v3/snapshot/"+url.PathEscape(snapshotID), query, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download snapshot request: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read snapshot response: %w", err)
	}

	if resp.StatusCode == http.StatusAccepted {
		var pending asyncResponse
		if err := json.Unmarshal(body, &pending); err != nil {
			return nil, fmt.Errorf("decode pending snapshot response: %w", err)
		}
		return &DownloadResponse{
			StatusCode:  resp.StatusCode,
			ContentType: resp.Header.Get("Content-Type"),
			Header:      resp.Header.Clone(),
			Ready:       false,
			Status:      pending.Status,
			Message:     pending.Message,
		}, nil
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, &APIError{StatusCode: resp.StatusCode, Body: string(body)}
	}

	return &DownloadResponse{
		StatusCode:  resp.StatusCode,
		ContentType: resp.Header.Get("Content-Type"),
		Header:      resp.Header.Clone(),
		Body:        body,
		Ready:       true,
	}, nil
}

// WaitAndDownload waits for a snapshot and then downloads the results.
func (c *Client) WaitAndDownload(ctx context.Context, snapshotID string, pollInterval time.Duration, opts DownloadOptions) (*DownloadResponse, error) {
	if _, err := c.WaitForSnapshot(ctx, snapshotID, pollInterval); err != nil {
		return nil, err
	}
	return c.DownloadSnapshot(ctx, snapshotID, opts)
}

// DeliverToAzure asks Bright Data to deliver a completed snapshot directly to
// Azure Blob Storage.
func (c *Client) DeliverToAzure(ctx context.Context, snapshotID string, opts AzureDeliveryOptions) (*DeliveryResponse, error) {
	snapshotID = strings.TrimSpace(snapshotID)
	if snapshotID == "" {
		return nil, errors.New("snapshot ID is required")
	}
	if err := opts.validate(); err != nil {
		return nil, err
	}

	filenameTemplate := strings.TrimSpace(opts.FilenameTemplate)
	if filenameTemplate == "" {
		filenameTemplate = snapshotID
	}
	extension := opts.Extension
	if extension == "" {
		extension = DeliveryExtensionJSON
	}

	payload := deliverSnapshotPayload{
		Deliver: azureDeliveryPayload{
			Type:      "azure",
			Filename:  azureFilenamePayload{Template: filenameTemplate, Extension: string(extension)},
			Container: opts.Container,
			Credentials: azureCredentialsPayload{
				Account:  opts.Account,
				Key:      opts.Key,
				SASToken: opts.SASToken,
			},
			Directory: strings.TrimSpace(opts.Directory),
		},
		Compress:  opts.Compress,
		BatchSize: opts.BatchSize,
	}

	req, err := c.newJSONRequest(ctx, http.MethodPost, "/datasets/v3/deliver/"+url.PathEscape(snapshotID), nil, payload)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("deliver snapshot request: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read deliver response: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, &APIError{StatusCode: resp.StatusCode, Body: string(body)}
	}

	var out DeliveryResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("decode deliver response: %w", err)
	}
	if strings.TrimSpace(out.DeliveryID) == "" {
		return nil, errors.New("deliver response missing delivery_id")
	}
	return &out, nil
}

// DeliveryStatus fetches the current state of a snapshot delivery request.
func (c *Client) DeliveryStatus(ctx context.Context, deliveryID string) (*DeliveryStatus, error) {
	deliveryID = strings.TrimSpace(deliveryID)
	if deliveryID == "" {
		return nil, errors.New("delivery ID is required")
	}

	req, err := c.newJSONRequest(ctx, http.MethodGet, "/datasets/v3/delivery/"+url.PathEscape(deliveryID), nil, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("delivery status request: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read delivery status response: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, &APIError{StatusCode: resp.StatusCode, Body: string(body)}
	}

	var out DeliveryStatus
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("decode delivery status response: %w", err)
	}
	return &out, nil
}

func (o AzureDeliveryOptions) validate() error {
	if strings.TrimSpace(o.Container) == "" {
		return errors.New("container is required")
	}
	if strings.TrimSpace(o.Account) == "" {
		return errors.New("storage account is required")
	}
	if strings.TrimSpace(o.Key) == "" && strings.TrimSpace(o.SASToken) == "" {
		return errors.New("storage key or sas token is required")
	}
	return nil
}

func validateDatasetAndInput(datasetID string, input any) error {
	if strings.TrimSpace(datasetID) == "" {
		return errors.New("dataset ID is required")
	}
	if input == nil {
		return errors.New("input is required")
	}
	return nil
}

func queryForRequest(datasetID string, notify, includeErrors bool, format Format, customOutputFields string) url.Values {
	query := url.Values{}
	query.Set("dataset_id", datasetID)
	query.Set("notify", strconv.FormatBool(notify))
	query.Set("include_errors", strconv.FormatBool(includeErrors))
	if format != "" {
		query.Set("format", string(format))
	}
	if strings.TrimSpace(customOutputFields) != "" {
		query.Set("custom_output_fields", strings.TrimSpace(customOutputFields))
	}
	return query
}

func (c *Client) newJSONRequest(ctx context.Context, method, path string, query url.Values, payload any) (*http.Request, error) {
	endpoint := c.baseURL + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}

	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("marshal request: %w", err)
		}
		body = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}
