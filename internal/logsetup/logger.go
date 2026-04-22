package logsetup

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"runtime/debug"
	"strings"
	"time"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel"
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
	otelExporterEndpointEnv  = "OTEL_EXPORTER_OTLP_ENDPOINT"
	telemetryShutdownTimeout = 5 * time.Second
	loggerName               = "careme/internal/logsetup"
	shortCommitLen           = 7
)

func Configure(ctx context.Context) (func(), error) {
	res, err := newResource()
	if err != nil {
		return nil, fmt.Errorf("build telemetry resource: %w", err)
	}

	traceProvider, err := newTracerProvider(ctx, res)
	if err != nil {
		return nil, fmt.Errorf("create tracer provider: %w", err)
	}
	otel.SetTracerProvider(traceProvider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	logProvider, err := newLoggerProvider(ctx, res)
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

func serviceVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}

	revision := strings.TrimSpace(buildSetting(info, "vcs.revision"))
	if revision == "" {
		return "unknown"
	}
	if len(revision) <= shortCommitLen {
		return revision
	}
	return revision[:shortCommitLen]
}

func buildSetting(info *debug.BuildInfo, key string) string {
	for _, setting := range info.Settings {
		if setting.Key == key {
			return setting.Value
		}
	}
	return ""
}

func newTracerProvider(ctx context.Context, res *resource.Resource) (*tracesdk.TracerProvider, error) {
	opts := []tracesdk.TracerProviderOption{tracesdk.WithResource(res)}
	if exportEnabled() {
		exporter, err := otlptracehttp.New(ctx)
		if err != nil {
			return nil, err
		}
		opts = append(opts, tracesdk.WithBatcher(exporter))
	}
	return tracesdk.NewTracerProvider(opts...), nil
}

func newLoggerProvider(ctx context.Context, res *resource.Resource) (*logsdk.LoggerProvider, error) {
	opts := []logsdk.LoggerProviderOption{logsdk.WithResource(res)}
	if exportEnabled() {
		exporter, err := otlploghttp.New(ctx)
		if err != nil {
			return nil, err
		}
		opts = append(opts, logsdk.WithProcessor(logsdk.NewBatchProcessor(exporter)))
	}
	return logsdk.NewLoggerProvider(opts...), nil
}

func newResource() (*resource.Resource, error) {
	return resource.Merge(resource.Default(), resource.NewWithAttributes("",
		semconv.ServiceName("careme"), semconv.ServiceVersion(serviceVersion())))
}

func exportEnabled() bool {
	return strings.TrimSpace(os.Getenv(otelExporterEndpointEnv)) != ""
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
