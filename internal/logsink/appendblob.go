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
	BlobName    string        // deault hostname/podname
	FlushEvery  time.Duration // default 2s
}

type Handler struct {
	ch     chan []byte
	wg     sync.WaitGroup
	ticker *time.Ticker
}

func New(ctx context.Context, cfg Config) (*Handler, error) {
	if cfg.AccountName == "" || cfg.AccountKey == "" || cfg.Container == "" {
		return nil, errors.New("AccountName, AccountKey, Container, and BlobName are required")
	}

	if cfg.BlobName == "" {
		cfg.BlobName, _ = os.Hostname()
	}

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

	h := &Handler{
		ch:     make(chan []byte, 1024), // Buffered channel to hold log entries
		ticker: time.NewTicker(cfg.FlushEvery),
	}
	h.wg.Add(1)
	go h.loop(ctx, ab)
	return h, nil

}

func (h *Handler) Close() error {
	close(h.ch)
	h.wg.Wait()
	h.ticker.Stop()
	return nil
}

// slog.Handler

func (h *Handler) Enabled(context.Context, slog.Level) bool { return true } // ignore levels

func (h *Handler) Handle(ctx context.Context, r slog.Record) error {
	var b bytes.Buffer
	json := slog.NewJSONHandler(&b, &slog.HandlerOptions{})
	//just send record to channel instead of json?
	err := json.Handle(ctx, r)
	if err != nil {
		return err
	}

	h.ch <- b.Bytes()
	os.Stdout.Write(b.Bytes())
	return nil
}

func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	// simplest: pre-add attrs on each record via wrapper
	return &withAttrs{Handler: h, attrs: attrs}
}

func (h *Handler) WithGroup(string) slog.Handler { return h } // no-op for simplicity

// internals

func (h *Handler) loop(ctx context.Context, ab *appendblob.Client) {
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
		case line, ok := <-h.ch:
			if !ok {
				flush() // seems redundant with h.ctx.Done() case, but whatever
				return
			}
			buf = append(buf, line...)
		case <-h.ticker.C:
			flush()
		}
	}
}

type withAttrs struct {
	slog.Handler
	attrs []slog.Attr
}

func (w *withAttrs) Handle(ctx context.Context, r slog.Record) error {
	r2 := r
	for _, a := range w.attrs {
		r2.AddAttrs(a)
	}
	return w.Handler.Handle(ctx, r2)
}

type readSeekNopCloser struct{ io.ReadSeeker }

func (r readSeekNopCloser) Close() error { return nil }
