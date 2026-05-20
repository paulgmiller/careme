package appinsightsexport

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"

	msappinsights "github.com/microsoft/ApplicationInsights-Go/appinsights"
	"github.com/microsoft/ApplicationInsights-Go/appinsights/contracts"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/sdk/resource"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

type appInsightsTraceExporter struct {
	client  msappinsights.TelemetryClient
	stopped bool
	mu      sync.RWMutex
	once    sync.Once
}

func NewTraceExporterFromEnv(res *resource.Resource, serviceVersion string) (tracesdk.SpanExporter, error) {
	cfg, err := LoadConfig()
	if err != nil {
		return nil, err
	}
	return NewTraceExporter(cfg, res, serviceVersion)
}

func NewTraceExporter(cfg *Config, res *resource.Resource, serviceVersion string) (tracesdk.SpanExporter, error) {
	client, err := newAppInsightsTelemetryClient(cfg)
	if err != nil {
		return nil, err
	}
	applyAppInsightsClientContext(client, res, serviceVersion)
	return &appInsightsTraceExporter{client: client}, nil
}

func (e *appInsightsTraceExporter) ExportSpans(_ context.Context, spans []tracesdk.ReadOnlySpan) error {
	e.mu.RLock()
	stopped := e.stopped
	e.mu.RUnlock()
	if stopped {
		return nil
	}

	for _, span := range spans {
		item := spanToAppInsights(span)
		if item == nil {
			continue
		}
		e.client.Track(item)
	}
	return nil
}

func (e *appInsightsTraceExporter) Shutdown(ctx context.Context) error {
	var err error
	e.once.Do(func() {
		e.mu.Lock()
		e.stopped = true
		e.mu.Unlock()
		err = closeTelemetryClient(ctx, e.client)
	})
	return err
}

func spanToAppInsights(span tracesdk.ReadOnlySpan) msappinsights.Telemetry {
	if span == nil {
		return nil
	}

	switch span.SpanKind() {
	case trace.SpanKindServer, trace.SpanKindConsumer:
		return serverSpanToRequest(span)
	default:
		return spanToDependency(span)
	}
}

func serverSpanToRequest(span tracesdk.ReadOnlySpan) msappinsights.Telemetry {
	method := spanAttrString(span, attrHTTPRequestMethod)
	requestURL := spanAttrString(span, attrURLFull, attrHTTPURLLegacy)
	responseCode := spanAttrCode(span)
	duration := span.EndTime().Sub(span.StartTime())

	request := msappinsights.NewRequestTelemetry(defaultString(method, "SPAN"), requestURL, duration, responseCode)
	request.Name = span.Name()
	request.Url = requestURL
	request.Id = span.SpanContext().SpanID().String()
	request.MarkTime(span.StartTime(), span.EndTime())
	request.Success = spanSuccess(span, responseCode)
	request.Source = spanAttrString(span, attrServerAddress, attrNetPeerName)
	copySpanAttributes(span, request.Properties, request.Measurements)
	populateSpanMetadata(span, request.Properties, request.Tags)
	return request
}

func spanToDependency(span tracesdk.ReadOnlySpan) msappinsights.Telemetry {
	target := spanAttrString(span, attrServerAddress, attrNetPeerName)
	fullURL := spanAttrString(span, attrURLFull, attrHTTPURLLegacy)
	if target == "" && fullURL != "" {
		if parsed, err := url.Parse(fullURL); err == nil {
			target = parsed.Host
		}
	}

	responseCode := spanAttrCode(span)
	dependency := msappinsights.NewRemoteDependencyTelemetry(
		span.Name(),
		dependencyType(span),
		target,
		spanSuccess(span, responseCode),
	)
	dependency.Id = span.SpanContext().SpanID().String()
	dependency.MarkTime(span.StartTime(), span.EndTime())
	dependency.ResultCode = responseCode
	dependency.Data = fullURL
	copySpanAttributes(span, dependency.Properties, dependency.Measurements)
	populateSpanMetadata(span, dependency.Properties, dependency.Tags)
	return dependency
}

func populateSpanMetadata(span tracesdk.ReadOnlySpan, properties map[string]string, tags contracts.ContextTags) {
	if span == nil {
		return
	}
	if tags == nil {
		tags = make(contracts.ContextTags)
	}
	tags.Operation().SetId(span.SpanContext().TraceID().String())
	tags.Operation().SetName(span.Name())
	if parent := span.Parent(); parent.IsValid() {
		tags.Operation().SetParentId(parent.SpanID().String())
	}

	properties["otel.span_id"] = span.SpanContext().SpanID().String()
	properties["otel.trace_id"] = span.SpanContext().TraceID().String()
	properties["otel.span_kind"] = span.SpanKind().String()
	properties["otel.status.code"] = span.Status().Code.String()
	if desc := strings.TrimSpace(span.Status().Description); desc != "" {
		properties["otel.status.description"] = desc
	}
	properties["otel.events.count"] = strconv.Itoa(len(span.Events()))
	properties["otel.links.count"] = strconv.Itoa(len(span.Links()))
	if scope := span.InstrumentationScope(); scope.Name != "" {
		properties["scope.name"] = scope.Name
	}
	if scope := span.InstrumentationScope(); scope.Version != "" {
		properties["scope.version"] = scope.Version
	}
	copyResourceAttributes(span.Resource(), properties, nil)
	if dropped := span.DroppedAttributes(); dropped > 0 {
		properties[propertyDroppedAttrCount] = strconv.Itoa(dropped)
	}
}

func copySpanAttributes(span tracesdk.ReadOnlySpan, properties map[string]string, measurements map[string]float64) {
	for _, kv := range span.Attributes() {
		appendAttributeValue(properties, measurements, string(kv.Key), kv.Value)
	}
}

func spanSuccess(span tracesdk.ReadOnlySpan, responseCode string) bool {
	if responseCode != "" {
		code, err := strconv.Atoi(responseCode)
		if err == nil {
			return code < http.StatusBadRequest || code == http.StatusUnauthorized
		}
	}
	return span.Status().Code != codes.Error
}

func dependencyType(span tracesdk.ReadOnlySpan) string {
	switch {
	case spanAttrString(span, attrDBSystemName, attrDBSystemLegacy) != "":
		return "DB"
	case spanAttrString(span, attrMessagingSystem) != "":
		return "Messaging"
	case spanAttrString(span, attrURLFull, attrHTTPURLLegacy) != "":
		return "HTTP"
	case span.SpanKind() == trace.SpanKindInternal:
		return "InProc"
	default:
		return strings.ToUpper(span.SpanKind().String())
	}
}

func spanAttrCode(span tracesdk.ReadOnlySpan) string {
	for _, kv := range span.Attributes() {
		if string(kv.Key) != attrHTTPResponseStatus {
			continue
		}
		switch kv.Value.Type() {
		case attribute.INT64:
			return strconv.FormatInt(kv.Value.AsInt64(), 10)
		case attribute.STRING:
			return kv.Value.AsString()
		}
	}
	return ""
}

func spanAttrString(span tracesdk.ReadOnlySpan, keys ...string) string {
	for _, key := range keys {
		for _, kv := range span.Attributes() {
			if string(kv.Key) != key {
				continue
			}
			if kv.Value.Type() == attribute.STRING {
				return kv.Value.AsString()
			}
			return fmt.Sprint(kv.Value.AsInterface())
		}
	}
	return ""
}
