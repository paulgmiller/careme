package logsetup

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	otellog "go.opentelemetry.io/otel/log"
	logsdk "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
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
	cfg, err := parseAppInsightsConnectionString("InstrumentationKey=ikey;IngestionEndpoint=https://westus.applicationinsights.azure.com/")
	require.NoError(t, err)
	assert.Equal(t, "ikey", cfg.InstrumentationKey)
	assert.Equal(t, "https://westus.applicationinsights.azure.com/", cfg.IngestionEndpoint.String())
}

func TestParseAppInsightsConnectionStringErrors(t *testing.T) {
	_, err := parseAppInsightsConnectionString("")
	require.EqualError(t, err, "connection string is empty")

	_, err = parseAppInsightsConnectionString("IngestionEndpoint=https://example.com/")
	require.EqualError(t, err, "instrumentation key is missing")

	_, err = parseAppInsightsConnectionString("InstrumentationKey=ikey")
	require.EqualError(t, err, "ingestion endpoint is missing")
}

func TestSelectedTelemetryMode(t *testing.T) {
	t.Setenv(appInsightsConnectionStringEnv, "")
	t.Setenv(otelExporterEndpointEnv, "")
	assert.Equal(t, telemetryModeLocal, selectedTelemetryMode())

	t.Setenv(otelExporterEndpointEnv, "https://example.com")
	assert.Equal(t, telemetryModeOTLP, selectedTelemetryMode())

	t.Setenv(appInsightsConnectionStringEnv, "InstrumentationKey=ikey;IngestionEndpoint=https://example.com/")
	assert.Equal(t, telemetryModeAppInsights, selectedTelemetryMode())
}

func TestAppInsightsTraceExporterExportsServerAndChildSpans(t *testing.T) {
	recorder := &ingestionRecorder{}

	res := resource.NewSchemaless(
		attribute.String(attrServiceName, "careme-test"),
		attribute.String(attrServiceVersion, "test-version"),
	)
	cfg := testAppInsightsConfig(t, recorder)
	exporter, err := newAppInsightsTraceExporter(cfg, res)
	require.NoError(t, err)

	provider := tracesdk.NewTracerProvider(tracesdk.WithSpanProcessor(tracesdk.NewSimpleSpanProcessor(exporter)))

	tracer := provider.Tracer("trace-test")
	ctx, root := tracer.Start(context.Background(), "GET /recipes", trace.WithSpanKind(trace.SpanKindServer))
	rootTraceID := root.SpanContext().TraceID().String()
	rootSpanID := root.SpanContext().SpanID().String()

	childCtx, child := tracer.Start(ctx, "GET pantry api", trace.WithSpanKind(trace.SpanKindClient))
	childSpanID := child.SpanContext().SpanID().String()
	child.SetAttributes(
		attribute.String(attrURLFull, "https://api.example.com/pantry"),
		attribute.Int(attrHTTPResponseStatus, 204),
	)
	child.End()

	_ = childCtx
	root.SetAttributes(
		attribute.String(attrHTTPRequestMethod, http.MethodGet),
		attribute.String(attrURLFull, "https://careme.test/recipes"),
		attribute.Int(attrHTTPResponseStatus, 200),
	)
	root.End()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, provider.Shutdown(shutdownCtx))

	envelopes := recorder.Envelopes()
	require.Len(t, envelopes, 2)

	request := findEnvelopeByBaseType(t, envelopes, "RequestData")
	assert.Equal(t, rootTraceID, request.Tags["ai.operation.id"])
	assert.Equal(t, rootSpanID, request.Data.BaseData["id"])
	assert.Equal(t, "GET /recipes", request.Data.BaseData["name"])

	dependency := findEnvelopeByBaseType(t, envelopes, "RemoteDependencyData")
	assert.Equal(t, rootTraceID, dependency.Tags["ai.operation.id"])
	assert.Equal(t, rootSpanID, dependency.Tags["ai.operation.parentId"])
	assert.Equal(t, childSpanID, dependency.Data.BaseData["id"])
	assert.Equal(t, "api.example.com", dependency.Data.BaseData["target"])
	assert.Equal(t, "HTTP", dependency.Data.BaseData["type"])
}

func TestAppInsightsLogExporterExportsTraceTelemetry(t *testing.T) {
	recorder := &ingestionRecorder{}

	res := resource.NewSchemaless(
		attribute.String(attrServiceName, "careme-test"),
		attribute.String(attrServiceVersion, "test-version"),
	)
	cfg := testAppInsightsConfig(t, recorder)
	exporter, err := newAppInsightsLogExporter(cfg, res)
	require.NoError(t, err)

	provider := logsdk.NewLoggerProvider(
		logsdk.WithResource(res),
		logsdk.WithProcessor(logsdk.NewSimpleProcessor(exporter)),
	)

	logger := provider.Logger("careme/logger", otellog.WithInstrumentationVersion("v1"))
	var record otellog.Record
	record.SetTimestamp(time.Unix(1700000000, 0))
	record.SetSeverity(otellog.SeverityWarn)
	record.SetSeverityText("WARN")
	record.SetBody(otellog.StringValue("chef alert"))
	record.AddAttributes(
		otellog.String("recipe", "stew"),
		otellog.Int64("attempt", 2),
	)

	ctx := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{1, 2, 3},
		SpanID:     trace.SpanID{4, 5, 6},
		TraceFlags: trace.FlagsSampled,
	}))
	logger.Emit(ctx, record)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, provider.Shutdown(shutdownCtx))

	envelopes := recorder.Envelopes()
	require.Len(t, envelopes, 1)
	message := envelopes[0]
	assert.Equal(t, "MessageData", message.Data.BaseType)
	assert.Equal(t, "01020300000000000000000000000000", message.Tags["ai.operation.id"])
	assert.Equal(t, "0405060000000000", message.Tags["ai.operation.parentId"])
	assert.Equal(t, "chef alert", message.Data.BaseData["message"])

	properties := nestedStringMap(message.Data.BaseData["properties"])
	assert.Equal(t, "stew", properties["recipe"])
	assert.Equal(t, "careme/logger", properties["logger.name"])
	assert.Equal(t, "2", properties["attempt"])
}

func TestConfigurePrefersAppInsightsOverOTLP(t *testing.T) {
	t.Setenv(appInsightsConnectionStringEnv, "InstrumentationKey=ikey;IngestionEndpoint=https://example.com/")
	t.Setenv(otelExporterEndpointEnv, "https://grafana.net/otlp")
	t.Setenv(otelExporterHeadersEnv, "")

	assert.Equal(t, telemetryModeAppInsights, selectedTelemetryMode())
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

func testAppInsightsConfig(t *testing.T, recorder *ingestionRecorder) *appInsightsConfig {
	t.Helper()
	ingestionURL, err := url.Parse("https://applicationinsights.test")
	require.NoError(t, err)
	return &appInsightsConfig{
		InstrumentationKey: "ikey",
		IngestionEndpoint:  ingestionURL,
		Client:             recorder.Client(),
	}
}
