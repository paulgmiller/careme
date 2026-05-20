package appinsightsexport

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	otellog "go.opentelemetry.io/otel/log"
	logsdk "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/trace"
)

func TestAppInsightsLogExporterExportsTraceTelemetry(t *testing.T) {
	recorder := &ingestionRecorder{}

	res := resource.NewSchemaless(
		attribute.String(attrServiceName, "careme-test"),
		attribute.String(attrServiceVersion, "test-version"),
	)
	cfg := testAppInsightsConfig(t, recorder)
	exporter, err := NewLogExporter(cfg, res, "test-version")
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
