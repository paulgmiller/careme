package logsink

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/appendblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/bloberror"
)

type Config struct {
	AccountName string
	AccountKey  string
	Container   string
	BlobName    string        // default hostname/podname
	FlushEvery  time.Duration // default 2s
}

type writer struct {
	ch     chan []byte
	done   chan bool
	wg     sync.WaitGroup
	ticker *time.Ticker
}

var _ io.WriteCloser = &writer{}

func New(ctx context.Context, cfg Config) (*writer, error) {
	if cfg.AccountName == "" || cfg.AccountKey == "" || cfg.Container == "" {
		return nil, errors.New("AccountName, AccountKey, and Container are required")
	}

	if cfg.BlobName == "" {
		cfg.BlobName, _ = os.Hostname()
	}

	// Add date-based folder structure: YYYY/MM/DD/hostname
	now := time.Now().UTC()
	dateFolder := FormatDateFolder(now.Year(), int(now.Month()), now.Day())
	cfg.BlobName = dateFolder + "/" + cfg.BlobName

	if cfg.FlushEvery <= 0 {
		cfg.FlushEvery = 2 * time.Second
	}

	cred, err := azblob.NewSharedKeyCredential(cfg.AccountName, cfg.AccountKey)
	if err != nil {
		return nil, err
	}
	blobURL := "https://" + cfg.AccountName + ".blob.core.windows.net/" +
		url.PathEscape(cfg.Container) + "/" + cfg.BlobName // BlobName may include slashes; don’t path-escape it.

	ab, err := appendblob.NewClientWithSharedKeyCredential(blobURL, cred, nil)
	if err != nil {
		return nil, err
	}
	_, err = ab.Create(ctx, nil) // ignore error; maybe already exists
	if err != nil {
		if !bloberror.HasCode(err, bloberror.BlobAlreadyExists) {
			return nil, err
		}
	}

	h := &writer{
		ch:     make(chan []byte, 1024), // Buffered channel to hold log entries
		done:   make(chan bool),         //tie this in with context.Cancel?
		ticker: time.NewTicker(cfg.FlushEvery),
	}
	h.wg.Add(1)
	go h.loop(ctx, ab)
	return h, nil

}

func NewJson(ctx context.Context, cfg Config) (slog.Handler, io.Closer, error) {
	blobappender, err := New(ctx, cfg)
	if err != nil {
		return nil, nil, err
	}
	return slog.NewJSONHandler(blobappender, &slog.HandlerOptions{
		AddSource: true,
	}), blobappender, nil
}

// Drain rest of logs. Will panic if called
func (h *writer) Close() error {
	close(h.done)
	h.wg.Wait()
	h.ticker.Stop()
	return nil
}

func (h *writer) Write(p []byte) (n int, err error) {
	// Copy because slog reuses its buffers after Write returns.
	line := make([]byte, len(p))
	copy(line, p)
	h.ch <- line
	//could err on closed but loggers just lose it anyways
	return len(p), nil
} // internals

func (h *writer) loop(ctx context.Context, ab *appendblob.Client) {
	defer h.wg.Done()
	var buf []byte
	flush := func() {
		if len(buf) == 0 {
			return
		}
		_, err := ab.AppendBlock(ctx, readSeekNopCloser{bytes.NewReader(buf)}, nil)
		if err != nil {
			fmt.Printf("error %s", err)
		}
		buf = buf[:0] //reset
	}

	for {
		select {
		case line := <-h.ch:
			buf = append(buf, line...)
		case <-h.ticker.C:
			flush()
		case <-h.done:
			//drain whats left
			for {
				select {
				case line := <-h.ch:
					buf = append(buf, line...)
				default:
					flush()
					return
				}
			}
		}
	}
}

type readSeekNopCloser struct{ io.ReadSeeker }

func (r readSeekNopCloser) Close() error { return nil }
