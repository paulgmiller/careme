package appinsightsexport

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

func TestAppInsightsTraceExporterExportsServerAndChildSpans(t *testing.T) {
	recorder := &ingestionRecorder{}

	res := resource.NewSchemaless(
		attribute.String(attrServiceName, "careme-test"),
		attribute.String(attrServiceVersion, "test-version"),
	)
	cfg := testAppInsightsConfig(t, recorder)
	exporter, err := NewTraceExporter(cfg, res, "test-version")
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
