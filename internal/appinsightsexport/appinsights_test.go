package appinsightsexport

import (
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type telemetryEnvelope struct {
	Name string            `json:"name"`
	Tags map[string]string `json:"tags"`
	Data struct {
		BaseType string         `json:"baseType"`
		BaseData map[string]any `json:"baseData"`
	} `json:"data"`
}

type ingestionRecorder struct {
	mu        sync.Mutex
	envelopes []telemetryEnvelope
}

func (r *ingestionRecorder) Envelopes() []telemetryEnvelope {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.envelopes)
}

func (r *ingestionRecorder) Client() *http.Client {
	return &http.Client{Transport: recorderTransport{recorder: r}}
}

type recorderTransport struct {
	recorder *ingestionRecorder
}

func (t recorderTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	defer func() {
		_ = req.Body.Close()
	}()
	reader := io.Reader(req.Body)
	if req.Header.Get("Content-Encoding") == "gzip" {
		gzipReader, err := gzip.NewReader(req.Body)
		if err != nil {
			return nil, err
		}
		defer func() {
			_ = gzipReader.Close()
		}()
		reader = gzipReader
	}
	body, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	var payload []telemetryEnvelope
	for decoder.More() {
		var envelope telemetryEnvelope
		if err := decoder.Decode(&envelope); err != nil {
			return nil, err
		}
		payload = append(payload, envelope)
	}
	t.recorder.mu.Lock()
	t.recorder.envelopes = append(t.recorder.envelopes, payload...)
	t.recorder.mu.Unlock()
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"itemsReceived":1,"itemsAccepted":1,"errors":[]}`)),
		Header:     make(http.Header),
		Request:    req,
	}, nil
}

func TestParseAppInsightsConnectionString(t *testing.T) {
	cfg, err := ParseConnectionString("InstrumentationKey=ikey;IngestionEndpoint=https://westus.applicationinsights.azure.com/")
	require.NoError(t, err)
	assert.Equal(t, "ikey", cfg.InstrumentationKey)
	assert.Equal(t, "https://westus.applicationinsights.azure.com/", cfg.IngestionEndpoint.String())
}

func TestParseAppInsightsConnectionStringErrors(t *testing.T) {
	_, err := ParseConnectionString("")
	require.EqualError(t, err, "connection string is empty")

	_, err = ParseConnectionString("IngestionEndpoint=https://example.com/")
	require.EqualError(t, err, "instrumentation key is missing")

	_, err = ParseConnectionString("InstrumentationKey=ikey")
	require.EqualError(t, err, "ingestion endpoint is missing")
}

func TestEnabled(t *testing.T) {
	t.Setenv(ConnectionStringEnv, "")
	assert.False(t, Enabled())

	t.Setenv(ConnectionStringEnv, "InstrumentationKey=ikey;IngestionEndpoint=https://example.com/")
	assert.True(t, Enabled())
}

func findEnvelopeByBaseType(t *testing.T, envelopes []telemetryEnvelope, baseType string) telemetryEnvelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.Data.BaseType == baseType {
			return envelope
		}
	}
	t.Fatalf("missing envelope with baseType %q", baseType)
	return telemetryEnvelope{}
}

func nestedStringMap(value any) map[string]string {
	raw, _ := value.(map[string]any)
	out := make(map[string]string, len(raw))
	for key, item := range raw {
		if str, ok := item.(string); ok {
			out[key] = str
		}
	}
	return out
}

func testAppInsightsConfig(t *testing.T, recorder *ingestionRecorder) *Config {
	t.Helper()
	ingestionURL, err := url.Parse("https://applicationinsights.test")
	require.NoError(t, err)
	return &Config{
		InstrumentationKey: "ikey",
		IngestionEndpoint:  ingestionURL,
		Client:             recorder.Client(),
	}
}
