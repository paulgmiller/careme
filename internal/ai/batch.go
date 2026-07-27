package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	openai "github.com/openai/openai-go/v3"
)

const (
	batchCollectionWindow = 500 * time.Millisecond
	batchPollInterval     = 15 * time.Second
	batchExecutionTimeout = 25 * time.Hour
)

type batchProcessingContextKey struct{}

// WithBatchProcessing routes supported OpenAI requests made with ctx through the
// Batch API. The API currently provides a 24-hour completion window, not a
// shorter completion guarantee.
func WithBatchProcessing(ctx context.Context) context.Context {
	return context.WithValue(ctx, batchProcessingContextKey{}, true)
}

func usesBatchProcessing(ctx context.Context) bool {
	enabled, _ := ctx.Value(batchProcessingContextKey{}).(bool)
	return enabled
}

type batchProcessor struct {
	client openai.Client

	mu      sync.Mutex
	pending map[openai.BatchNewParamsEndpoint]*pendingBatch
}

type pendingBatch struct {
	items []*batchItem
	timer *time.Timer
}

type batchItem struct {
	ctx    context.Context
	body   any
	result chan batchResult
}

type batchResult struct {
	body json.RawMessage
	err  error
}

type batchInput struct {
	CustomID string `json:"custom_id"`
	Method   string `json:"method"`
	URL      string `json:"url"`
	Body     any    `json:"body"`
}

type batchOutput struct {
	CustomID string `json:"custom_id"`
	Response *struct {
		StatusCode int             `json:"status_code"`
		RequestID  string          `json:"request_id"`
		Body       json.RawMessage `json:"body"`
	} `json:"response"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func newBatchProcessor(client openai.Client) *batchProcessor {
	return &batchProcessor{
		client:  client,
		pending: make(map[openai.BatchNewParamsEndpoint]*pendingBatch),
	}
}

func (p *batchProcessor) submit(ctx context.Context, endpoint openai.BatchNewParamsEndpoint, body, output any) error {
	item := &batchItem{
		ctx:    ctx,
		body:   body,
		result: make(chan batchResult, 1),
	}
	p.enqueue(endpoint, item)

	select {
	case <-ctx.Done():
		return ctx.Err()
	case result := <-item.result:
		if result.err != nil {
			return result.err
		}
		if err := json.Unmarshal(result.body, output); err != nil {
			return fmt.Errorf("decode OpenAI batch response: %w", err)
		}
		return nil
	}
}

func (p *batchProcessor) enqueue(endpoint openai.BatchNewParamsEndpoint, item *batchItem) {
	p.mu.Lock()
	defer p.mu.Unlock()

	pending := p.pending[endpoint]
	if pending == nil {
		pending = &pendingBatch{}
		p.pending[endpoint] = pending
		pending.timer = time.AfterFunc(batchCollectionWindow, func() {
			p.flush(endpoint, pending)
		})
	}
	pending.items = append(pending.items, item)
}

func (p *batchProcessor) flush(endpoint openai.BatchNewParamsEndpoint, pending *pendingBatch) {
	p.mu.Lock()
	if p.pending[endpoint] != pending {
		p.mu.Unlock()
		return
	}
	delete(p.pending, endpoint)
	items := append([]*batchItem(nil), pending.items...)
	p.mu.Unlock()

	active := items[:0]
	for _, item := range items {
		if err := item.ctx.Err(); err != nil {
			item.result <- batchResult{err: err}
			continue
		}
		active = append(active, item)
	}
	if len(active) == 0 {
		return
	}

	ctx, cancel := context.WithTimeout(context.WithoutCancel(active[0].ctx), batchExecutionTimeout)
	defer cancel()
	p.execute(ctx, endpoint, active)
}

func (p *batchProcessor) execute(ctx context.Context, endpoint openai.BatchNewParamsEndpoint, items []*batchItem) {
	input, customIDs, err := marshalBatchInput(endpoint, items)
	if err != nil {
		deliverBatchError(items, err)
		return
	}

	file, err := p.client.Files.New(ctx, openai.FileNewParams{
		File:    openai.File(bytes.NewReader(input), "careme-batch.jsonl", "application/jsonl"),
		Purpose: openai.FilePurposeBatch,
		ExpiresAfter: openai.FileNewParamsExpiresAfter{
			Seconds: int64((48 * time.Hour).Seconds()),
		},
	})
	if err != nil {
		deliverBatchError(items, fmt.Errorf("upload OpenAI batch input: %w", err))
		return
	}

	batch, err := p.client.Batches.New(ctx, openai.BatchNewParams{
		CompletionWindow: openai.BatchNewParamsCompletionWindow24h,
		Endpoint:         endpoint,
		InputFileID:      file.ID,
		OutputExpiresAfter: openai.BatchNewParamsOutputExpiresAfter{
			Seconds: int64((48 * time.Hour).Seconds()),
		},
	})
	if err != nil {
		deliverBatchError(items, fmt.Errorf("create OpenAI batch: %w", err))
		return
	}
	slog.InfoContext(ctx, "submitted OpenAI batch", "batch_id", batch.ID, "endpoint", endpoint, "request_count", len(items))

	batch, err = p.waitForBatch(ctx, batch)
	if err != nil {
		deliverBatchError(items, err)
		return
	}

	results, err := p.readBatchResults(ctx, batch)
	if err != nil {
		deliverBatchError(items, err)
		return
	}
	for i, item := range items {
		result, ok := results[customIDs[i]]
		if !ok {
			result.err = fmt.Errorf("OpenAI batch %s returned no result for %s", batch.ID, customIDs[i])
		}
		item.result <- result
	}
}

func marshalBatchInput(endpoint openai.BatchNewParamsEndpoint, items []*batchItem) ([]byte, []string, error) {
	var input bytes.Buffer
	encoder := json.NewEncoder(&input)
	customIDs := make([]string, len(items))
	for i, item := range items {
		customID := fmt.Sprintf("request-%d", i+1)
		customIDs[i] = customID
		if err := encoder.Encode(batchInput{
			CustomID: customID,
			Method:   http.MethodPost,
			URL:      string(endpoint),
			Body:     item.body,
		}); err != nil {
			return nil, nil, fmt.Errorf("encode OpenAI batch input: %w", err)
		}
	}
	return input.Bytes(), customIDs, nil
}

func (p *batchProcessor) waitForBatch(ctx context.Context, batch *openai.Batch) (*openai.Batch, error) {
	for {
		switch batch.Status {
		case openai.BatchStatusCompleted:
			slog.InfoContext(ctx, "completed OpenAI batch", "batch_id", batch.ID, "completed", batch.RequestCounts.Completed, "failed", batch.RequestCounts.Failed)
			return batch, nil
		case openai.BatchStatusFailed, openai.BatchStatusExpired, openai.BatchStatusCancelled:
			return nil, batchTerminalError(batch)
		}

		timer := time.NewTimer(batchPollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			cancelCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if _, err := p.client.Batches.Cancel(cancelCtx, batch.ID); err != nil {
				slog.ErrorContext(ctx, "failed to cancel timed out OpenAI batch", "batch_id", batch.ID, "error", err)
			}
			return nil, fmt.Errorf("wait for OpenAI batch %s: %w", batch.ID, ctx.Err())
		case <-timer.C:
		}

		var err error
		batch, err = p.client.Batches.Get(ctx, batch.ID)
		if err != nil {
			return nil, fmt.Errorf("get OpenAI batch %s: %w", batch.ID, err)
		}
	}
}

func batchTerminalError(batch *openai.Batch) error {
	details := make([]string, 0, len(batch.Errors.Data))
	for _, batchErr := range batch.Errors.Data {
		details = append(details, batchErr.Message)
	}
	if len(details) == 0 {
		return fmt.Errorf("OpenAI batch %s ended with status %s", batch.ID, batch.Status)
	}
	return fmt.Errorf("OpenAI batch %s ended with status %s: %s", batch.ID, batch.Status, strings.Join(details, "; "))
}

func (p *batchProcessor) readBatchResults(ctx context.Context, batch *openai.Batch) (map[string]batchResult, error) {
	results := make(map[string]batchResult, int(batch.RequestCounts.Total))
	for _, fileID := range []string{batch.OutputFileID, batch.ErrorFileID} {
		if strings.TrimSpace(fileID) == "" {
			continue
		}
		response, err := p.client.Files.Content(ctx, fileID)
		if err != nil {
			return nil, fmt.Errorf("download OpenAI batch result file %s: %w", fileID, err)
		}
		err = decodeBatchOutput(response.Body, results)
		closeErr := response.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("decode OpenAI batch result file %s: %w", fileID, err)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("close OpenAI batch result file %s: %w", fileID, closeErr)
		}
	}
	return results, nil
}

func decodeBatchOutput(reader io.Reader, results map[string]batchResult) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), 200*1024*1024)
	for scanner.Scan() {
		var output batchOutput
		if err := json.Unmarshal(scanner.Bytes(), &output); err != nil {
			return err
		}
		switch {
		case output.Error != nil:
			results[output.CustomID] = batchResult{err: fmt.Errorf("OpenAI batch request %s failed (%s): %s", output.CustomID, output.Error.Code, output.Error.Message)}
		case output.Response == nil:
			results[output.CustomID] = batchResult{err: fmt.Errorf("OpenAI batch request %s returned neither a response nor an error", output.CustomID)}
		case output.Response.StatusCode < http.StatusOK || output.Response.StatusCode >= http.StatusMultipleChoices:
			results[output.CustomID] = batchResult{err: fmt.Errorf("OpenAI batch request %s returned HTTP %d: %s", output.CustomID, output.Response.StatusCode, output.Response.Body)}
		default:
			results[output.CustomID] = batchResult{body: output.Response.Body}
		}
	}
	return scanner.Err()
}

func deliverBatchError(items []*batchItem, err error) {
	if err == nil {
		err = errors.New("unknown OpenAI batch error")
	}
	for _, item := range items {
		item.result <- batchResult{err: err}
	}
}
