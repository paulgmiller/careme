package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/stretchr/testify/require"
)

func TestBatchProcessorCoalescesRequestsAndMatchesResultsByCustomID(t *testing.T) {
	var mu sync.Mutex
	var uploaded []batchInput
	batchCreates := 0
	var createdBatch openai.BatchNewParams

	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/files"):
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				return nil, fmt.Errorf("parse file upload: %w", err)
			}
			file, _, err := r.FormFile("file")
			if err != nil {
				return nil, fmt.Errorf("read uploaded file: %w", err)
			}

			decoder := json.NewDecoder(file)
			for {
				var input batchInput
				err := decoder.Decode(&input)
				if err == io.EOF {
					break
				}
				if err != nil {
					_ = file.Close()
					return nil, fmt.Errorf("decode batch input: %w", err)
				}
				uploaded = append(uploaded, input)
			}
			if err := file.Close(); err != nil {
				return nil, fmt.Errorf("close uploaded file: %w", err)
			}
			return testJSONResponse(r, `{"id":"file-input","bytes":100,"created_at":1,"filename":"careme-batch.jsonl","object":"file","purpose":"batch","status":"processed"}`), nil
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/batches"):
			mu.Lock()
			batchCreates++
			mu.Unlock()

			if err := json.NewDecoder(r.Body).Decode(&createdBatch); err != nil {
				return nil, fmt.Errorf("decode batch create: %w", err)
			}
			return testJSONResponse(r, `{
				"id":"batch-1",
				"completion_window":"24h",
				"created_at":1,
				"endpoint":"/v1/responses",
				"input_file_id":"file-input",
				"object":"batch",
				"status":"completed",
				"output_file_id":"file-output",
				"request_counts":{"total":2,"completed":2,"failed":0}
			}`), nil
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/files/file-output/content"):
			var body strings.Builder
			for _, input := range uploaded {
				inputBody, ok := input.Body.(map[string]any)
				if !ok {
					return nil, fmt.Errorf("unexpected batch input body: %#v", input.Body)
				}
				resultBody, err := json.Marshal(map[string]string{"value": fmt.Sprintf("result-%.0f", inputBody["index"])})
				if err != nil {
					return nil, err
				}
				_, _ = fmt.Fprintf(&body, `{"custom_id":%q,"response":{"status_code":200,"request_id":"req","body":%s},"error":null}`+"\n", input.CustomID, resultBody)
			}
			response := testJSONResponse(r, body.String())
			response.Header.Set("Content-Type", "application/binary")
			return response, nil
		default:
			return nil, fmt.Errorf("unexpected OpenAI request: %s %s", r.Method, r.URL.Path)
		}
	})

	processor := newBatchProcessor(openai.NewClient(
		option.WithAPIKey("test-key"),
		option.WithHTTPClient(&http.Client{Transport: transport}),
	))

	type output struct {
		Value string `json:"value"`
	}
	results := make([]output, 2)
	errs := make([]error, 2)
	var requests sync.WaitGroup
	for i := range results {
		requests.Go(func() {
			errs[i] = processor.submit(t.Context(), openai.BatchNewParamsEndpointV1Responses, map[string]int{"index": i}, &results[i])
		})
	}
	requests.Wait()

	require.NoError(t, errs[0])
	require.NoError(t, errs[1])
	require.Equal(t, "result-0", results[0].Value)
	require.Equal(t, "result-1", results[1].Value)
	require.Len(t, uploaded, 2)
	require.Equal(t, http.MethodPost, uploaded[0].Method)
	require.Equal(t, "/v1/responses", uploaded[0].URL)
	require.Equal(t, 1, batchCreates)
	require.Equal(t, openai.BatchNewParamsCompletionWindow24h, createdBatch.CompletionWindow)
	require.Equal(t, openai.BatchNewParamsEndpointV1Responses, createdBatch.Endpoint)
	require.Equal(t, "file-input", createdBatch.InputFileID)
}

func TestDecodeBatchOutputReturnsPerRequestErrors(t *testing.T) {
	results := make(map[string]batchResult)
	err := decodeBatchOutput(strings.NewReader(
		`{"custom_id":"request-1","response":{"status_code":429,"body":{"error":"rate limited"}},"error":null}`+"\n"+
			`{"custom_id":"request-2","response":null,"error":{"code":"batch_expired","message":"too late"}}`+"\n",
	), results)

	require.NoError(t, err)
	require.ErrorContains(t, results["request-1"].err, "HTTP 429")
	require.ErrorContains(t, results["request-2"].err, "batch_expired")
}

func TestWithBatchProcessingPreservesContextAndMarksIt(t *testing.T) {
	type contextKey struct{}
	ctx := context.WithValue(t.Context(), contextKey{}, "value")

	batchCtx := WithBatchProcessing(ctx)

	require.True(t, usesBatchProcessing(batchCtx))
	require.Equal(t, "value", batchCtx.Value(contextKey{}))
	require.False(t, usesBatchProcessing(ctx))
}

func testJSONResponse(request *http.Request, body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    request,
	}
}
