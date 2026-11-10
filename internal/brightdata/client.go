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
	defaultBaseURL      = "https://api.brightdata.com"
	defaultPollInterval = 2 * time.Second
)

type format string

const (
	formatJSON   format = "json"
	formatNDJSON format = "ndjson"
	formatCSV    format = "csv"
)

type snapshotStatus string

const (
	snapshotStatusStarting snapshotStatus = "starting"
	snapshotStatusRunning  snapshotStatus = "running"
	snapshotStatusBuilding snapshotStatus = "building"
	snapshotStatusReady    snapshotStatus = "ready"
	snapshotStatusFailed   snapshotStatus = "failed"
)

// client calls the Bright Data scraper API.
type client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

// apiError captures a non-success Bright Data response.
type apiError struct {
	StatusCode int
	Body       string
}

func (e *apiError) Error() string {
	body := strings.TrimSpace(e.Body)
	if body == "" {
		return fmt.Sprintf("bright data request failed: status %d", e.StatusCode)
	}
	return fmt.Sprintf("bright data request failed: status %d: %s", e.StatusCode, body)
}

type scrapeOptions struct {
	Notify             bool
	IncludeErrors      bool
	Format             format
	CustomOutputFields string
}

type triggerOptions struct {
	Notify             bool
	IncludeErrors      bool
	Format             format
	CustomOutputFields string
}

type downloadOptions struct {
	Format format
}

type scrapeResponse struct {
	StatusCode  int
	ContentType string
	Header      http.Header
	Body        []byte
	SnapshotID  string
	Message     string
}

type triggerResponse struct {
	SnapshotID string `json:"snapshot_id"`
}

type progressResponse struct {
	SnapshotID   string         `json:"snapshot_id"`
	DatasetID    string         `json:"dataset_id"`
	Status       snapshotStatus `json:"status"`
	ErrorMessage string         `json:"error_message,omitempty"`
}

func (r *progressResponse) ready() bool {
	return r != nil && r.Status == snapshotStatusReady
}

func (r *progressResponse) failed() bool {
	return r != nil && r.Status == snapshotStatusFailed
}

type downloadResponse struct {
	StatusCode  int
	ContentType string
	Header      http.Header
	Body        []byte
	Ready       bool
	Status      snapshotStatus
	Message     string
}

type deliveryExtension string

const (
	deliveryExtensionJSON   deliveryExtension = "json"
	deliveryExtensionNDJSON deliveryExtension = "ndjson"
	deliveryExtensionCSV    deliveryExtension = "csv"
)

type azureDeliveryOptions struct {
	Container        string
	Account          string
	Key              string
	SASToken         string
	Directory        string
	FilenameTemplate string
	Extension        deliveryExtension
	Compress         bool
	BatchSize        int
}

type deliveryResponse struct {
	DeliveryID string `json:"delivery_id"`
}

type asyncResponse struct {
	SnapshotID string         `json:"snapshot_id"`
	Status     snapshotStatus `json:"status,omitempty"`
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

// newClient creates a Bright Data client with the default API base URL.
func newClient(apiKey string, httpClient *http.Client) (*client, error) {
	return newClientWithBaseURL(defaultBaseURL, apiKey, httpClient)
}

// newClientWithBaseURL creates a Bright Data client for the provided base URL.
func newClientWithBaseURL(baseURL, apiKey string, httpClient *http.Client) (*client, error) {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, errors.New("api key is required")
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}

	return &client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		apiKey:     apiKey,
		httpClient: httpClient,
	}, nil
}

// scrape sends a synchronous scrape request. When Bright Data cannot finish
// within the sync timeout, it returns a snapshot ID that can be polled later.
func (c *client) scrape(ctx context.Context, datasetID string, input any, opts scrapeOptions) (*scrapeResponse, error) {
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
		return &scrapeResponse{
			StatusCode:  resp.StatusCode,
			ContentType: resp.Header.Get("Content-Type"),
			Header:      resp.Header.Clone(),
			Body:        body,
			SnapshotID:  async.SnapshotID,
			Message:     async.Message,
		}, nil
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, &apiError{StatusCode: resp.StatusCode, Body: string(body)}
	}

	return &scrapeResponse{
		StatusCode:  resp.StatusCode,
		ContentType: resp.Header.Get("Content-Type"),
		Header:      resp.Header.Clone(),
		Body:        body,
	}, nil
}

// trigger starts an asynchronous scrape and returns the snapshot ID.
func (c *client) trigger(ctx context.Context, datasetID string, input any, opts triggerOptions) (*triggerResponse, error) {
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
		return nil, &apiError{StatusCode: resp.StatusCode, Body: string(body)}
	}

	var out triggerResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("decode trigger response: %w", err)
	}
	if strings.TrimSpace(out.SnapshotID) == "" {
		return nil, errors.New("trigger response missing snapshot_id")
	}
	return &out, nil
}

// progress fetches the status of an asynchronous snapshot.
func (c *client) progress(ctx context.Context, snapshotID string) (*progressResponse, error) {
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
		return nil, &apiError{StatusCode: resp.StatusCode, Body: string(body)}
	}

	var out progressResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("decode progress response: %w", err)
	}
	return &out, nil
}

// waitForSnapshot polls progress until the snapshot is ready or failed.
func (c *client) waitForSnapshot(ctx context.Context, snapshotID string, pollInterval time.Duration) (*progressResponse, error) {
	if pollInterval <= 0 {
		pollInterval = defaultPollInterval
	}

	for {
		progress, err := c.progress(ctx, snapshotID)
		if err != nil {
			return nil, err
		}
		if progress.ready() {
			return progress, nil
		}
		if progress.failed() {
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

// downloadSnapshot retrieves a completed snapshot body. If the snapshot is not
// ready yet, Bright Data responds with HTTP 202 and a status message.
func (c *client) downloadSnapshot(ctx context.Context, snapshotID string, opts downloadOptions) (*downloadResponse, error) {
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
		return &downloadResponse{
			StatusCode:  resp.StatusCode,
			ContentType: resp.Header.Get("Content-Type"),
			Header:      resp.Header.Clone(),
			Ready:       false,
			Status:      pending.Status,
			Message:     pending.Message,
		}, nil
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, &apiError{StatusCode: resp.StatusCode, Body: string(body)}
	}

	return &downloadResponse{
		StatusCode:  resp.StatusCode,
		ContentType: resp.Header.Get("Content-Type"),
		Header:      resp.Header.Clone(),
		Body:        body,
		Ready:       true,
	}, nil
}

// waitAndDownload waits for a snapshot and then downloads the results.
func (c *client) waitAndDownload(ctx context.Context, snapshotID string, pollInterval time.Duration, opts downloadOptions) (*downloadResponse, error) {
	if _, err := c.waitForSnapshot(ctx, snapshotID, pollInterval); err != nil {
		return nil, err
	}
	return c.downloadSnapshot(ctx, snapshotID, opts)
}

// deliverToAzure asks Bright Data to deliver a completed snapshot directly to
// Azure Blob Storage.
func (c *client) deliverToAzure(ctx context.Context, snapshotID string, opts azureDeliveryOptions) (*deliveryResponse, error) {
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
		extension = deliveryExtensionJSON
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
		return nil, &apiError{StatusCode: resp.StatusCode, Body: string(body)}
	}

	var out deliveryResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("decode deliver response: %w", err)
	}
	if strings.TrimSpace(out.DeliveryID) == "" {
		return nil, errors.New("deliver response missing delivery_id")
	}
	return &out, nil
}

func (o azureDeliveryOptions) validate() error {
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

func queryForRequest(datasetID string, notify, includeErrors bool, format format, customOutputFields string) url.Values {
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

func (c *client) newJSONRequest(ctx context.Context, method, path string, query url.Values, payload any) (*http.Request, error) {
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
