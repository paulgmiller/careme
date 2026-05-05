package logsetup

import (
	"context"
	"testing"
	"time"

	"careme/internal/appinsightsexport"

	"go.opentelemetry.io/otel/sdk/resource"
)

func TestProvidersBuildWhenAppInsightsAndOTLPAreEnabled(t *testing.T) {
	t.Setenv(appinsightsexport.ConnectionStringEnv, "InstrumentationKey=ikey;IngestionEndpoint=https://example.com/")
	t.Setenv(otelExporterEndpointEnv, "http://localhost:4318")

	res := resource.Empty()
	traceProvider, err := newTraceProvider(context.Background(), res)
	if err != nil {
		t.Fatalf("new trace provider: %v", err)
	}
	logProvider, err := newLogProvider(context.Background(), res)
	if err != nil {
		t.Fatalf("new log provider: %v", err)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := logProvider.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("shutdown log provider: %v", err)
	}
	if err := traceProvider.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("shutdown trace provider: %v", err)
	}
}

func TestConfigureStillValidatesOTLPWhenAppInsightsIsEnabled(t *testing.T) {
	t.Setenv(appinsightsexport.ConnectionStringEnv, "InstrumentationKey=ikey;IngestionEndpoint=https://example.com/")
	t.Setenv(otelExporterEndpointEnv, "https://example.grafana.net/otlp")
	t.Setenv(otelExporterHeadersEnv, "")

	if _, err := Configure(context.Background()); err == nil {
		t.Fatal("expected grafana OTLP config validation error")
	}
}
