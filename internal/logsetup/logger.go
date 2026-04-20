package logsetup

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	otlplogglobal "go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/propagation"
	logsdk "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
)

const (
	otelServiceNameEnv            = "OTEL_SERVICE_NAME"
	otelExporterEndpointEnv       = "OTEL_EXPORTER_OTLP_ENDPOINT"
	otelExporterTracesEnv         = "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"
	otelExporterLogsEnv           = "OTEL_EXPORTER_OTLP_LOGS_ENDPOINT"
	azureMonitorTracesEndpointEnv = "AZURE_MONITOR_OTLP_TRACES_ENDPOINT"
	azureMonitorLogsEndpointEnv   = "AZURE_MONITOR_OTLP_LOGS_ENDPOINT"
	azureMonitorScope             = "https://monitor.azure.com/.default"
	telemetryShutdownTimeout      = 5 * time.Second
	loggerName                    = "careme/internal/logsetup"
)

func Configure(ctx context.Context) (func(), error) {
	res, err := newResource()
	if err != nil {
		return nil, fmt.Errorf("build telemetry resource: %w", err)
	}

	azureCredential, err := newAzureMonitorCredential()
	if err != nil {
		return nil, fmt.Errorf("create azure monitor credential: %w", err)
	}

	traceProvider, err := newTracerProvider(ctx, res, azureCredential)
	if err != nil {
		return nil, fmt.Errorf("create tracer provider: %w", err)
	}
	otel.SetTracerProvider(traceProvider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	logProvider, err := newLoggerProvider(ctx, res, azureCredential)
	if err != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), telemetryShutdownTimeout)
		defer cancel()
		_ = traceProvider.Shutdown(shutdownCtx)
		return nil, fmt.Errorf("create logger provider: %w", err)
	}
	otlplogglobal.SetLoggerProvider(logProvider)

	handlers := []slog.Handler{
		newContextHandler(slog.NewTextHandler(os.Stdout, nil)),
		newContextHandler(otelslog.NewHandler(
			loggerName,
			otelslog.WithLoggerProvider(logProvider),
			otelslog.WithVersion(serviceVersion()),
		)),
	}

	slog.SetDefault(slog.New(slog.NewMultiHandler(handlers...)))
	return recoverAndClose(ctx, func(shutdownCtx context.Context) error {
		return errors.Join(
			logProvider.Shutdown(shutdownCtx),
			traceProvider.Shutdown(shutdownCtx),
		)
	}), nil
}

func newTracerProvider(ctx context.Context, res *resource.Resource, azureCredential azcore.TokenCredential) (*tracesdk.TracerProvider, error) {
	opts := []tracesdk.TracerProviderOption{tracesdk.WithResource(res)}
	exporter, err := newTraceExporter(ctx, azureCredential)
	if err != nil {
		return nil, err
	}
	if exporter != nil {
		opts = append(opts, tracesdk.WithBatcher(exporter))
	}
	return tracesdk.NewTracerProvider(opts...), nil
}

func newLoggerProvider(ctx context.Context, res *resource.Resource, azureCredential azcore.TokenCredential) (*logsdk.LoggerProvider, error) {
	opts := []logsdk.LoggerProviderOption{logsdk.WithResource(res)}
	exporter, err := newLogExporter(ctx, azureCredential)
	if err != nil {
		return nil, err
	}
	if exporter != nil {
		opts = append(opts, logsdk.WithProcessor(logsdk.NewBatchProcessor(exporter)))
	}
	return logsdk.NewLoggerProvider(opts...), nil
}

func newTraceExporter(ctx context.Context, azureCredential azcore.TokenCredential) (tracesdk.SpanExporter, error) {
	if endpoint := strings.TrimSpace(os.Getenv(azureMonitorTracesEndpointEnv)); endpoint != "" {
		return otlptracehttp.New(ctx,
			otlptracehttp.WithEndpointURL(endpoint),
			otlptracehttp.WithHTTPClient(newAzureMonitorHTTPClient(azureCredential)),
		)
	}
	if tracesExportEnabled() {
		exporter, err := otlptracehttp.New(ctx)
		if err != nil {
			return nil, err
		}
		return exporter, nil
	}
	return nil, nil
}

func newLogExporter(ctx context.Context, azureCredential azcore.TokenCredential) (logsdk.Exporter, error) {
	if endpoint := strings.TrimSpace(os.Getenv(azureMonitorLogsEndpointEnv)); endpoint != "" {
		return otlploghttp.New(ctx,
			otlploghttp.WithEndpointURL(endpoint),
			otlploghttp.WithHTTPClient(newAzureMonitorHTTPClient(azureCredential)),
		)
	}
	if logsExportEnabled() {
		exporter, err := otlploghttp.New(ctx)
		if err != nil {
			return nil, err
		}
		return exporter, nil
	}
	return nil, nil
}

func newResource() (*resource.Resource, error) {
	attrs := []attribute.KeyValue{semconv.ServiceName(serviceName())}
	if version := serviceVersion(); version != "" {
		attrs = append(attrs, semconv.ServiceVersion(version))
	}
	return resource.Merge(resource.Default(), resource.NewWithAttributes("", attrs...))
}

func serviceName() string {
	if name := strings.TrimSpace(os.Getenv(otelServiceNameEnv)); name != "" {
		return name
	}
	if len(os.Args) == 0 {
		return "careme"
	}
	name := filepath.Base(os.Args[0])
	name = strings.TrimSpace(strings.TrimSuffix(name, filepath.Ext(name)))
	if name == "" {
		return "careme"
	}
	return name
}

func serviceVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	version := strings.TrimSpace(info.Main.Version)
	if version == "(devel)" {
		return ""
	}
	return version
}

func tracesExportEnabled() bool {
	return strings.TrimSpace(os.Getenv(otelExporterEndpointEnv)) != "" ||
		strings.TrimSpace(os.Getenv(otelExporterTracesEnv)) != "" ||
		strings.TrimSpace(os.Getenv(azureMonitorTracesEndpointEnv)) != ""
}

func logsExportEnabled() bool {
	return strings.TrimSpace(os.Getenv(otelExporterEndpointEnv)) != "" ||
		strings.TrimSpace(os.Getenv(otelExporterLogsEnv)) != "" ||
		strings.TrimSpace(os.Getenv(azureMonitorLogsEndpointEnv)) != ""
}

func azureMonitorExportEnabled() bool {
	return strings.TrimSpace(os.Getenv(azureMonitorTracesEndpointEnv)) != "" ||
		strings.TrimSpace(os.Getenv(azureMonitorLogsEndpointEnv)) != ""
}

func newAzureMonitorCredential() (azcore.TokenCredential, error) {
	if !azureMonitorExportEnabled() {
		return nil, nil
	}
	return azidentity.NewDefaultAzureCredential(nil)
}

func newAzureMonitorHTTPClient(credential azcore.TokenCredential) *http.Client {
	return &http.Client{
		Transport: &azureMonitorTransport{
			base:       http.DefaultTransport,
			credential: credential,
		},
	}
}

type azureMonitorTransport struct {
	base       http.RoundTripper
	credential azcore.TokenCredential
}

func (t *azureMonitorTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	token, err := t.credential.GetToken(req.Context(), policy.TokenRequestOptions{
		Scopes: []string{azureMonitorScope},
	})
	if err != nil {
		return nil, fmt.Errorf("get azure monitor token: %w", err)
	}

	cloned := req.Clone(req.Context())
	cloned.Header = req.Header.Clone()
	cloned.Header.Set("Authorization", "Bearer "+token.Token)

	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(cloned)
}

func recoverAndClose(ctx context.Context, closeFn func(context.Context) error) func() {
	return func() {
		panicValue := recover()
		if panicValue != nil {
			slog.ErrorContext(ctx, "panic before logger flush",
				"panic", panicValue,
				"stack", string(debug.Stack()),
			)
		}

		shutdownCtx, cancel := context.WithTimeout(context.Background(), telemetryShutdownTimeout)
		defer cancel()
		if err := closeFn(shutdownCtx); err != nil {
			slog.ErrorContext(ctx, "telemetry shutdown failed", "error", err)
		}

		if panicValue != nil {
			panic(panicValue)
		}
	}
}
