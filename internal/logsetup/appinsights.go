package logsetup

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	msappinsights "github.com/microsoft/ApplicationInsights-Go/appinsights"
	"github.com/microsoft/ApplicationInsights-Go/appinsights/contracts"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	otellog "go.opentelemetry.io/otel/log"
	logsdk "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

const (
	appInsightsConnectionStringEnv = "APPLICATIONINSIGHTS_CONNECTION_STRING"
	appInsightsTrackPath           = "/v2/track"
	appInsightsBatchSize           = 1024
	appInsightsBatchInterval       = 10 * time.Second
)

const (
	attrDBSystemName         = "db.system.name"
	attrDBSystemLegacy       = "db.system"
	attrHTTPResponseStatus   = "http.response.status_code"
	attrHTTPRequestMethod    = "http.request.method"
	attrHTTPURLLegacy        = "http.url"
	attrMessagingSystem      = "messaging.system"
	attrNetPeerName          = "net.peer.name"
	attrServerAddress        = "server.address"
	attrURLFull              = "url.full"
	attrServiceName          = "service.name"
	attrServiceVersion       = "service.version"
	propertyDroppedAttrCount = "otel.dropped_attributes"
)

type telemetryMode int

const (
	telemetryModeLocal telemetryMode = iota
	telemetryModeAppInsights
	telemetryModeOTLP
)

type appInsightsConfig struct {
	InstrumentationKey string
	IngestionEndpoint  *url.URL
	Client             *http.Client
}

type appInsightsTraceExporter struct {
	client  msappinsights.TelemetryClient
	stopped bool
	mu      sync.RWMutex
	once    sync.Once
}

type appInsightsLogExporter struct {
	client  msappinsights.TelemetryClient
	stopped bool
	mu      sync.RWMutex
	once    sync.Once
}

func selectedTelemetryMode() telemetryMode {
	if appInsightsEnabled() {
		return telemetryModeAppInsights
	}
	if exportEnabled() {
		return telemetryModeOTLP
	}
	return telemetryModeLocal
}

func appInsightsEnabled() bool {
	return strings.TrimSpace(os.Getenv(appInsightsConnectionStringEnv)) != ""
}

func parseAppInsightsConnectionString(connectionString string) (*appInsightsConfig, error) {
	connectionString = strings.TrimSpace(connectionString)
	if connectionString == "" {
		return nil, errors.New("connection string is empty")
	}

	var instrumentationKey string
	var ingestionEndpoint string
	for _, field := range strings.Split(connectionString, ";") {
		pair := strings.SplitN(strings.TrimSpace(field), "=", 2)
		if len(pair) != 2 {
			continue
		}
		switch pair[0] {
		case "InstrumentationKey":
			instrumentationKey = strings.TrimSpace(pair[1])
		case "IngestionEndpoint":
			ingestionEndpoint = strings.TrimSpace(pair[1])
		}
	}

	if instrumentationKey == "" {
		return nil, errors.New("instrumentation key is missing")
	}
	if ingestionEndpoint == "" {
		return nil, errors.New("ingestion endpoint is missing")
	}

	u, err := url.Parse(ingestionEndpoint)
	if err != nil {
		return nil, fmt.Errorf("ingestion endpoint is not a valid URL: %w", err)
	}
	return &appInsightsConfig{
		InstrumentationKey: instrumentationKey,
		IngestionEndpoint:  u,
	}, nil
}

func loadAppInsightsConfig() (*appInsightsConfig, error) {
	return parseAppInsightsConnectionString(os.Getenv(appInsightsConnectionStringEnv))
}

func newAppInsightsTracerProvider(res *resource.Resource) (*tracesdk.TracerProvider, error) {
	cfg, err := loadAppInsightsConfig()
	if err != nil {
		return nil, err
	}
	exporter, err := newAppInsightsTraceExporter(cfg, res)
	if err != nil {
		return nil, err
	}
	return tracesdk.NewTracerProvider(
		tracesdk.WithResource(res),
		tracesdk.WithBatcher(exporter),
	), nil
}

func newAppInsightsLoggerProvider(res *resource.Resource) (*logsdk.LoggerProvider, error) {
	cfg, err := loadAppInsightsConfig()
	if err != nil {
		return nil, err
	}
	exporter, err := newAppInsightsLogExporter(cfg, res)
	if err != nil {
		return nil, err
	}
	return logsdk.NewLoggerProvider(
		logsdk.WithResource(res),
		logsdk.WithProcessor(logsdk.NewBatchProcessor(exporter)),
	), nil
}

func newAppInsightsTraceExporter(cfg *appInsightsConfig, res *resource.Resource) (*appInsightsTraceExporter, error) {
	client, err := newAppInsightsTelemetryClient(cfg, res)
	if err != nil {
		return nil, err
	}
	return &appInsightsTraceExporter{client: client}, nil
}

func newAppInsightsLogExporter(cfg *appInsightsConfig, res *resource.Resource) (*appInsightsLogExporter, error) {
	client, err := newAppInsightsTelemetryClient(cfg, res)
	if err != nil {
		return nil, err
	}
	return &appInsightsLogExporter{client: client}, nil
}

func newAppInsightsTelemetryClient(cfg *appInsightsConfig, res *resource.Resource) (msappinsights.TelemetryClient, error) {
	if cfg == nil {
		return nil, errors.New("app insights config is required")
	}
	endpoint := *cfg.IngestionEndpoint
	endpoint.Path = appInsightsTrackPath

	telemetryConfig := &msappinsights.TelemetryConfiguration{
		InstrumentationKey: cfg.InstrumentationKey,
		EndpointUrl:        endpoint.String(),
		MaxBatchSize:       appInsightsBatchSize,
		MaxBatchInterval:   appInsightsBatchInterval,
		Client:             cfg.Client,
	}
	if telemetryConfig.Client == nil {
		telemetryConfig.Client = http.DefaultClient
	}

	client := msappinsights.NewTelemetryClientFromConfig(telemetryConfig)
	applyAppInsightsClientContext(client, res)
	return client, nil
}

func applyAppInsightsClientContext(client msappinsights.TelemetryClient, res *resource.Resource) {
	if client == nil {
		return
	}
	ctx := client.Context()
	if ctx == nil {
		return
	}

	ctx.Tags.Cloud().SetRole(resourceString(res, attrServiceName, "careme"))
	if version := resourceString(res, attrServiceVersion, serviceVersion()); version != "" {
		ctx.CommonProperties[attrServiceVersion] = version
	}
	if service := resourceString(res, attrServiceName, "careme"); service != "" {
		ctx.CommonProperties[attrServiceName] = service
	}
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

func closeTelemetryClient(ctx context.Context, client msappinsights.TelemetryClient) error {
	if client == nil {
		return nil
	}

	var done <-chan struct{}
	if deadline, ok := ctx.Deadline(); ok {
		retryTimeout := time.Until(deadline)
		if retryTimeout < 0 {
			retryTimeout = 0
		}
		done = client.Channel().Close(retryTimeout)
	} else {
		done = client.Channel().Close()
	}

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
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

func copySpanAttributes(span tracesdk.ReadOnlySpan, properties map[string]string, measurements map[string]float64) {
	for _, kv := range span.Attributes() {
		appendAttributeValue(properties, measurements, string(kv.Key), kv.Value)
	}
}

func copyResourceAttributes(res *resource.Resource, properties map[string]string, measurements map[string]float64) {
	if res == nil {
		return
	}
	for iter := res.Iter(); iter.Next(); {
		kv := iter.Attribute()
		appendAttributeValue(properties, measurements, "resource."+string(kv.Key), kv.Value)
	}
}

func appendAttributeValue(properties map[string]string, measurements map[string]float64, key string, value attribute.Value) {
	switch value.Type() {
	case attribute.BOOL:
		properties[key] = strconv.FormatBool(value.AsBool())
	case attribute.INT64:
		if measurements != nil {
			measurements[key] = float64(value.AsInt64())
		} else {
			properties[key] = strconv.FormatInt(value.AsInt64(), 10)
		}
	case attribute.FLOAT64:
		if measurements != nil {
			measurements[key] = value.AsFloat64()
		} else {
			properties[key] = strconv.FormatFloat(value.AsFloat64(), 'g', -1, 64)
		}
	case attribute.STRING:
		properties[key] = value.AsString()
	default:
		properties[key] = fmt.Sprint(value.AsInterface())
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

func resourceString(res *resource.Resource, key, fallback string) string {
	if res == nil {
		return fallback
	}
	for iter := res.Iter(); iter.Next(); {
		kv := iter.Attribute()
		if string(kv.Key) == key && kv.Value.Type() == attribute.STRING {
			return kv.Value.AsString()
		}
	}
	return fallback
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func firstNonZero(values ...time.Time) time.Time {
	for _, value := range values {
		if !value.IsZero() {
			return value
		}
	}
	return time.Time{}
}
