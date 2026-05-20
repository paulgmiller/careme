package appinsightsexport

import (
	"context"
	"fmt"
	"strconv"
	"sync"

	msappinsights "github.com/microsoft/ApplicationInsights-Go/appinsights"
	"github.com/microsoft/ApplicationInsights-Go/appinsights/contracts"
	otellog "go.opentelemetry.io/otel/log"
	logsdk "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
)

type appInsightsLogExporter struct {
	client  msappinsights.TelemetryClient
	stopped bool
	mu      sync.RWMutex
	once    sync.Once
}

func NewLogExporterFromEnv(res *resource.Resource, serviceVersion string) (logsdk.Exporter, error) {
	cfg, err := LoadConfig()
	if err != nil {
		return nil, err
	}
	return NewLogExporter(cfg, res, serviceVersion)
}

func NewLogExporter(cfg *Config, res *resource.Resource, serviceVersion string) (logsdk.Exporter, error) {
	client, err := newAppInsightsTelemetryClient(cfg)
	if err != nil {
		return nil, err
	}
	applyAppInsightsClientContext(client, res, serviceVersion)
	return &appInsightsLogExporter{client: client}, nil
}

func (e *appInsightsLogExporter) Export(_ context.Context, records []logsdk.Record) error {
	e.mu.RLock()
	stopped := e.stopped
	e.mu.RUnlock()
	if stopped {
		return nil
	}

	for _, record := range records {
		item := logRecordToAppInsights(record)
		e.client.Track(item)
	}
	return nil
}

func (e *appInsightsLogExporter) Shutdown(ctx context.Context) error {
	var err error
	e.once.Do(func() {
		e.mu.Lock()
		e.stopped = true
		e.mu.Unlock()
		err = closeTelemetryClient(ctx, e.client)
	})
	return err
}

func (e *appInsightsLogExporter) ForceFlush(context.Context) error {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.stopped || e.client == nil {
		return nil
	}
	e.client.Channel().Flush()
	return nil
}

func logRecordToAppInsights(record logsdk.Record) *msappinsights.TraceTelemetry {
	message := record.Body().String()
	if message == "" || message == "<nil>" {
		message = record.EventName()
	}
	traceItem := msappinsights.NewTraceTelemetry(message, appInsightsSeverityForLog(record))
	if ts := firstNonZero(record.Timestamp(), record.ObservedTimestamp()); !ts.IsZero() {
		traceItem.Timestamp = ts
	}
	record.WalkAttributes(func(kv otellog.KeyValue) bool {
		appendLogValue(traceItem.Properties, kv.Key, kv.Value)
		return true
	})
	populateLogMetadata(record, traceItem.Properties, traceItem.Tags)
	return traceItem
}

func populateLogMetadata(record logsdk.Record, properties map[string]string, tags contracts.ContextTags) {
	if tags == nil {
		tags = make(contracts.ContextTags)
	}
	if traceID := record.TraceID(); traceID.IsValid() {
		tags.Operation().SetId(traceID.String())
		properties["otel.trace_id"] = traceID.String()
	}
	if spanID := record.SpanID(); spanID.IsValid() {
		tags.Operation().SetParentId(spanID.String())
		properties["otel.span_id"] = spanID.String()
	}
	if eventName := record.EventName(); eventName != "" {
		properties["otel.event_name"] = eventName
	}
	if text := record.SeverityText(); text != "" {
		properties["otel.severity_text"] = text
	}
	properties["otel.trace_flags"] = fmt.Sprintf("%02x", byte(record.TraceFlags()))
	if err := record.Body().String(); err == "<nil>" {
		properties["otel.body.empty"] = "true"
	}
	if scope := record.InstrumentationScope(); scope.Name != "" {
		properties["logger.name"] = scope.Name
	}
	if scope := record.InstrumentationScope(); scope.Version != "" {
		properties["logger.version"] = scope.Version
	}
	copyResourceAttributes(record.Resource(), properties, nil)
	if dropped := record.DroppedAttributes(); dropped > 0 {
		properties[propertyDroppedAttrCount] = strconv.Itoa(dropped)
	}
}

func appendLogValue(properties map[string]string, key string, value otellog.Value) {
	switch value.Kind() {
	case otellog.KindBool:
		properties[key] = strconv.FormatBool(value.AsBool())
	case otellog.KindInt64:
		properties[key] = strconv.FormatInt(value.AsInt64(), 10)
	case otellog.KindFloat64:
		properties[key] = strconv.FormatFloat(value.AsFloat64(), 'g', -1, 64)
	case otellog.KindString:
		properties[key] = value.AsString()
	default:
		properties[key] = value.String()
	}
}

func appInsightsSeverityForLog(record logsdk.Record) contracts.SeverityLevel {
	switch severity := record.Severity(); {
	case severity >= otellog.SeverityFatal:
		return msappinsights.Critical
	case severity >= otellog.SeverityError:
		return msappinsights.Error
	case severity >= otellog.SeverityWarn:
		return msappinsights.Warning
	case severity >= otellog.SeverityInfo:
		return msappinsights.Information
	default:
		return msappinsights.Verbose
	}
}
