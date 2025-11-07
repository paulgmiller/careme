package logs

import (
	"bufio"
	"careme/internal/logsink"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blob"
)

// LogEntry represents a single log entry
type LogEntry struct {
	Time    string                 `json:"time"`
	Level   string                 `json:"level"`
	Msg     string                 `json:"msg"`
	Source  map[string]interface{} `json:"source,omitempty"`
	Extra   map[string]interface{} `json:"-"`
	RawJSON string                 `json:"-"`
}

// UnmarshalJSON implements custom JSON unmarshaling to capture all fields
func (l *LogEntry) UnmarshalJSON(data []byte) error {
	// First unmarshal into the defined fields
	type Alias LogEntry
	aux := &struct {
		*Alias
	}{
		Alias: (*Alias)(l),
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	// Then unmarshal into a map to capture all fields
	var allFields map[string]interface{}
	if err := json.Unmarshal(data, &allFields); err != nil {
		return err
	}

	// Store the raw JSON for display
	l.RawJSON = string(data)

	// Store extra fields that weren't explicitly defined
	l.Extra = make(map[string]interface{})
	for k, v := range allFields {
		if k != "time" && k != "level" && k != "msg" && k != "source" {
			l.Extra[k] = v
		}
	}

	return nil
}

// Config holds configuration for log reader
type Config struct {
	AccountName string
	AccountKey  string
	Container   string
}

// Reader reads logs from Azure Blob Storage
type Reader struct {
	config Config
	client *azblob.Client
}

// NewReader creates a new log reader
func NewReader(ctx context.Context, cfg Config) (*Reader, error) {
	if cfg.AccountName == "" || cfg.AccountKey == "" || cfg.Container == "" {
		return nil, errors.New("AccountName, AccountKey, and Container are required")
	}

	cred, err := azblob.NewSharedKeyCredential(cfg.AccountName, cfg.AccountKey)
	if err != nil {
		return nil, err
	}

	serviceURL := fmt.Sprintf("https://%s.blob.core.windows.net/", cfg.AccountName)
	client, err := azblob.NewClientWithSharedKeyCredential(serviceURL, cred, nil)
	if err != nil {
		return nil, err
	}

	return &Reader{
		config: cfg,
		client: client,
	}, nil
}

// GetLogs retrieves logs from the last N hours
func (r *Reader) GetLogs(ctx context.Context, hours int) ([]LogEntry, error) {
	if hours <= 0 {
		hours = 24 // default to 24 hours
	}

	since := time.Now().Add(-time.Duration(hours) * time.Hour)
	var allLogs []LogEntry

	// Generate date prefixes to query (covering the time range)
	datePrefixes := r.getDatePrefixes(since, time.Now())

	// List blobs using date-based prefixes for efficiency
	for _, prefix := range datePrefixes {
		pager := r.client.NewListBlobsFlatPager(r.config.Container, &azblob.ListBlobsFlatOptions{
			Prefix:  &prefix,
			Include: azblob.ListBlobsInclude{Metadata: true},
		})

		for pager.More() {
			resp, err := pager.NextPage(ctx)
			if err != nil {
				return nil, fmt.Errorf("failed to list blobs: %w", err)
			}

			for _, blobItem := range resp.Segment.BlobItems {
				// Skip blobs that haven't been modified in the time range (optimization)
				if blobItem.Properties.LastModified != nil && blobItem.Properties.LastModified.Before(since) {
					continue
				}

				// Read the blob content
				logs, err := r.readBlobLogs(ctx, *blobItem.Name, since)
				if err != nil {
					// Log error but continue with other blobs
					fmt.Printf("error reading blob %s: %v\n", *blobItem.Name, err)
					continue
				}
				allLogs = append(allLogs, logs...)
			}
		}
	}

	return allLogs, nil
}

// getDatePrefixes generates date folder prefixes for the time range
func (r *Reader) getDatePrefixes(since, until time.Time) []string {
	var prefixes []string
	current := since.UTC().Truncate(24 * time.Hour)
	end := until.UTC().Truncate(24 * time.Hour)

	for !current.After(end) {
		prefix := logsink.FormatDateFolder(current.Year(), int(current.Month()), current.Day()) + "/"
		prefixes = append(prefixes, prefix)
		current = current.Add(24 * time.Hour)
	}

	return prefixes
}

// readBlobLogs reads and parses log entries from a specific blob
func (r *Reader) readBlobLogs(ctx context.Context, blobName string, since time.Time) ([]LogEntry, error) {
	blobURL := fmt.Sprintf("https://%s.blob.core.windows.net/%s/%s",
		r.config.AccountName,
		url.PathEscape(r.config.Container),
		blobName)

	cred, err := azblob.NewSharedKeyCredential(r.config.AccountName, r.config.AccountKey)
	if err != nil {
		return nil, err
	}

	blobClient, err := blob.NewClientWithSharedKeyCredential(blobURL, cred, nil)
	if err != nil {
		return nil, err
	}

	// Download the blob
	resp, err := blobClient.DownloadStream(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to download blob: %w", err)
	}
	defer resp.Body.Close()

	return r.parseLogStream(resp.Body, since)
}

// parseLogStream parses log entries from a reader
func (r *Reader) parseLogStream(reader io.Reader, since time.Time) ([]LogEntry, error) {
	var logs []LogEntry
	scanner := bufio.NewScanner(reader)
	// Increase buffer size for potentially large log lines
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var entry LogEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			// Skip invalid JSON lines
			continue
		}

		// Parse time and filter
		if entry.Time != "" {
			logTime, err := time.Parse(time.RFC3339Nano, entry.Time)
			if err == nil && logTime.Before(since) {
				continue
			}
		}

		logs = append(logs, entry)
	}

	if err := scanner.Err(); err != nil {
		return logs, fmt.Errorf("error scanning logs: %w", err)
	}

	return logs, nil
}
