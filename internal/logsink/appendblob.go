package appendblobhandler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/appendblob"
)

type Config struct {
	AccountName string
	AccountKey  string
	Container   string
	BlobName    string        // e.g. "serviceA/prod/logs.jsonl" use
	FlushEvery  time.Duration // default 2s

}

type Handler struct {
	cfg    Config
	ab     *appendblob.Client
	ch     chan []byte
	ctx    context.Context
	cancel context.CancelFunc
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

	ctx, cancel := context.WithCancel(ctx)
	h := &Handler{
		cfg:    cfg,
		ab:     ab,
		ch:     make(chan []byte, 1024), // Buffered channel to hold log entries
		ctx:    ctx,
		cancel: cancel,
		ticker: time.NewTicker(cfg.FlushEvery),
	}
	h.wg.Add(1)
	go h.loop()
	return h, nil
}

func (h *Handler) Close() error {
	h.cancel()
	close(h.ch)
	h.wg.Wait()
	h.ticker.Stop()
	return nil
}

// slog.Handler

func (h *Handler) Enabled(context.Context, slog.Level) bool { return true } // ignore levels

func (h *Handler) Handle(_ context.Context, r slog.Record) error {
	ev := make(map[string]any, r.NumAttrs()+3) //magic number!
	ts := r.Time
	if ts.IsZero() {
		ts = time.Now()
	}
	ev["ts"] = ts.UTC().Format(time.RFC3339Nano)
	ev["msg"] = r.Message

	r.Attrs(func(a slog.Attr) bool {
		a.Value = a.Value.Resolve()
		if a.Value.Kind() == slog.KindGroup {
			m := map[string]any{}
			//only goes one level deep. do we care?
			for _, aa := range a.Value.Group() {
				aa.Value = aa.Value.Resolve()
				m[aa.Key] = aa.Value.Any()
			}
			ev[a.Key] = m
		} else {
			ev[a.Key] = a.Value.Any()
		}
		return true
	})

	//just send record to channel instead of json?
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(ev); err != nil {
		return err
	}

	select {
	case h.ch <- append([]byte{}, b.Bytes()...):
		return nil
	case <-h.ctx.Done():
		return h.ctx.Err()
	}
}

func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	// simplest: pre-add attrs on each record via wrapper
	return &withAttrs{Handler: h, attrs: attrs}
}

func (h *Handler) WithGroup(string) slog.Handler { return h } // no-op for simplicity

// internals

func (h *Handler) loop() {
	defer h.wg.Done()
	var buf []byte
	flush := func() {
		if len(buf) == 0 {
			return
		}
		_, _ = h.ab.AppendBlock(h.ctx, readSeekNopCloser{bytes.NewReader(buf)}, nil)
		buf = buf[:0] //reset
	}

	for {
		select {
		case <-h.ctx.Done():
			flush()
			return
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
