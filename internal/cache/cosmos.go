package cache

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	cosmosAPIVersion        = "2018-12-31"
	cosmosQueryContentType  = "application/query+json"
	cosmosDocumentMediaType = "application/json"
)

type CosmosCache struct {
	endpoint  *url.URL
	client    *http.Client
	key       string
	database  string
	container string
}

type cosmosDocument struct {
	ID        string `json:"id"`
	Partition string `json:"partition"`
	Value     string `json:"value"`
}

type cosmosQuery struct {
	Query      string                 `json:"query"`
	Parameters []cosmosQueryParameter `json:"parameters"`
}

type cosmosQueryParameter struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type cosmosQueryResponse struct {
	Documents []cosmosDocument `json:"Documents"`
}

var _ ListCache = (*CosmosCache)(nil)

func NewCosmosCache(database, container string) (*CosmosCache, error) {
	endpoint, ok := os.LookupEnv("AZURE_COSMOS_ENDPOINT")
	if !ok {
		return nil, fmt.Errorf("AZURE_COSMOS_ENDPOINT could not be found")
	}

	key, ok := os.LookupEnv("AZURE_COSMOS_KEY")
	if !ok {
		return nil, fmt.Errorf("AZURE_COSMOS_KEY could not be found")
	}

	parsedEndpoint, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("invalid AZURE_COSMOS_ENDPOINT: %w", err)
	}

	return &CosmosCache{
		endpoint:  parsedEndpoint,
		client:    http.DefaultClient,
		key:       key,
		database:  database,
		container: container,
	}, nil
}

func (cc *CosmosCache) List(ctx context.Context, prefix string, _ string) ([]string, error) {
	query := cosmosQuery{
		Query: "SELECT c.id FROM c WHERE STARTSWITH(c.id, @prefix)",
		Parameters: []cosmosQueryParameter{
			{Name: "@prefix", Value: prefix},
		},
	}

	resp, err := cc.queryDocuments(ctx, query)
	if err != nil {
		return nil, err
	}

	keys := make([]string, 0, len(resp.Documents))
	for _, doc := range resp.Documents {
		keys = append(keys, strings.TrimPrefix(doc.ID, prefix))
	}
	return keys, nil
}

func (cc *CosmosCache) Exists(ctx context.Context, key string) (bool, error) {
	_, err := cc.readDocument(ctx, key)
	if err != nil {
		if err == ErrNotFound {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (cc *CosmosCache) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	doc, err := cc.readDocument(ctx, key)
	if err != nil {
		return nil, err
	}
	return io.NopCloser(strings.NewReader(doc.Value)), nil
}

func (cc *CosmosCache) Put(ctx context.Context, key, value string, opts PutOptions) error {
	doc := cosmosDocument{
		ID:        key,
		Partition: partitionKey(key),
		Value:     value,
	}

	body, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("failed to marshal document: %w", err)
	}

	if opts.Condition == PutIfNoneMatch {
		resp, err := cc.doRequest(ctx, http.MethodPost, cc.documentsPath(), "docs", cc.documentsResourceID(), bytes.NewReader(body), func(req *http.Request) {
			req.Header.Set("Content-Type", cosmosDocumentMediaType)
			cc.setPartitionKeyHeader(req, doc.Partition)
		})
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusConflict {
			return ErrAlreadyExists
		}
		if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
			return cc.decodeCosmosError(resp)
		}
		return nil
	}

	resp, err := cc.doRequest(ctx, http.MethodPost, cc.documentsPath(), "docs", cc.documentsResourceID(), bytes.NewReader(body), func(req *http.Request) {
		req.Header.Set("Content-Type", cosmosDocumentMediaType)
		req.Header.Set("x-ms-documentdb-is-upsert", "true")
		cc.setPartitionKeyHeader(req, doc.Partition)
	})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return cc.decodeCosmosError(resp)
	}
	return nil
}

func (cc *CosmosCache) readDocument(ctx context.Context, key string) (*cosmosDocument, error) {
	path := cc.documentPath(key)
	resourceID := cc.documentResourceID(key)
	resp, err := cc.doRequest(ctx, http.MethodGet, path, "docs", resourceID, nil, func(req *http.Request) {
		cc.setPartitionKeyHeader(req, partitionKey(key))
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrNotFound
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, cc.decodeCosmosError(resp)
	}

	var doc cosmosDocument
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return nil, fmt.Errorf("failed to decode document: %w", err)
	}
	return &doc, nil
}

func (cc *CosmosCache) queryDocuments(ctx context.Context, query cosmosQuery) (*cosmosQueryResponse, error) {
	body, err := json.Marshal(query)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal query: %w", err)
	}

	resp, err := cc.doRequest(ctx, http.MethodPost, cc.documentsPath(), "docs", cc.documentsResourceID(), bytes.NewReader(body), func(req *http.Request) {
		req.Header.Set("Content-Type", cosmosQueryContentType)
		req.Header.Set("x-ms-documentdb-isquery", "true")
		req.Header.Set("x-ms-documentdb-query-enablecrosspartition", "true")
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, cc.decodeCosmosError(resp)
	}

	var parsed cosmosQueryResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("failed to decode query response: %w", err)
	}
	return &parsed, nil
}

func (cc *CosmosCache) documentsPath() string {
	return fmt.Sprintf("/dbs/%s/colls/%s/docs", cc.database, cc.container)
}

func (cc *CosmosCache) documentPath(key string) string {
	return fmt.Sprintf("/dbs/%s/colls/%s/docs/%s", cc.database, cc.container, url.PathEscape(key))
}

func (cc *CosmosCache) documentsResourceID() string {
	return fmt.Sprintf("dbs/%s/colls/%s", cc.database, cc.container)
}

func (cc *CosmosCache) documentResourceID(key string) string {
	return fmt.Sprintf("dbs/%s/colls/%s/docs/%s", cc.database, cc.container, key)
}

func (cc *CosmosCache) setPartitionKeyHeader(req *http.Request, partition string) {
	payload, _ := json.Marshal([]string{partition})
	req.Header.Set("x-ms-documentdb-partitionkey", string(payload))
}

func (cc *CosmosCache) doRequest(ctx context.Context, method, path, resourceType, resourceID string, body io.Reader, extraHeaders func(*http.Request)) (*http.Response, error) {
	reqURL := cc.endpoint.ResolveReference(&url.URL{Path: path})
	req, err := http.NewRequestWithContext(ctx, method, reqURL.String(), body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	date := time.Now().UTC().Format(http.TimeFormat)
	req.Header.Set("x-ms-date", date)
	req.Header.Set("x-ms-version", cosmosAPIVersion)
	req.Header.Set("Authorization", cc.authHeader(method, resourceType, resourceID, date))
	if extraHeaders != nil {
		extraHeaders(req)
	}

	resp, err := cc.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cosmos request failed: %w", err)
	}
	return resp, nil
}

func (cc *CosmosCache) authHeader(method, resourceType, resourceID, date string) string {
	payload := strings.ToLower(method) + "\n" +
		strings.ToLower(resourceType) + "\n" +
		resourceID + "\n" +
		strings.ToLower(date) + "\n\n"

	key, _ := base64.StdEncoding.DecodeString(cc.key)
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(payload))
	signature := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	token := fmt.Sprintf("type=master&ver=1.0&sig=%s", signature)
	return url.QueryEscape(token)
}

func (cc *CosmosCache) decodeCosmosError(resp *http.Response) error {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("cosmos error status %d", resp.StatusCode)
	}
	return fmt.Errorf("cosmos error status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
}

func partitionKey(key string) string {
	if key == "" {
		return ""
	}
	if idx := strings.Index(key, "/"); idx >= 0 {
		return key[:idx]
	}
	return key
}
