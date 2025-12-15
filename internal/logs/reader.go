package logs

import (
	"careme/internal/logsink"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
)

// Reader reads logs from Azure Blob Storage
type Reader struct {
	config *logsink.Config
	client *azblob.Client
}

// NewReader creates a new log reader
func NewReader(ctx context.Context, cfg *logsink.Config) (*Reader, error) {
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
func (r *Reader) GetLogs(ctx context.Context, hours int, w io.Writer) error {
	if hours <= 0 {
		return errors.New("hours must be positive")
	}

	since := time.Now().Add(-time.Duration(hours) * time.Hour)

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
				return fmt.Errorf("failed to list blobs: %w", err)
			}

			for _, blobItem := range resp.Segment.BlobItems {
				// Skip blobs that haven't been modified in the time range (optimization)
				if blobItem.Properties.LastModified != nil && blobItem.Properties.LastModified.Before(since) {
					continue
				}

				// Read the blob content
				err := r.readBlobLogs(ctx, *blobItem.Name, w)
				if err != nil {
					// Log error but continue with other blobs
					slog.ErrorContext(ctx, "error reading blob", "blob", *blobItem.Name, "error", err)
					continue
				}

			}
		}
	}

	return nil
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
// can we parallelize this without busting the writer?
func (r *Reader) readBlobLogs(ctx context.Context, blobName string, w io.Writer) error {

	blobClient := r.client.ServiceClient().NewContainerClient(r.config.Container).NewBlobClient(blobName)

	// Download the blob
	resp, err := blobClient.DownloadStream(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to download blob: %w", err)
	}
	defer resp.Body.Close()

	_, err = io.Copy(w, resp.Body)
	return err
}
