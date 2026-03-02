package main

import (
	"strings"
	"testing"
)

func TestParseAppInsightsConnectionString(t *testing.T) {
	connectionString := "InstrumentationKey=test-key;IngestionEndpoint=https://westus3-1.in.applicationinsights.azure.com/;LiveEndpoint=https://westus3.livediagnostics.monitor.azure.com/;ApplicationId=app-id"
	cfg, err := parseAppInsightsConnectionString(connectionString)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.InstrumentationKey != "test-key" {
		t.Fatalf("expected instrumentation key test-key, got %q", cfg.InstrumentationKey)
	}
	if cfg.EndpointUrl != "https://westus3-1.in.applicationinsights.azure.com/v2/track" {
		t.Fatalf("unexpected ingestion endpoint: %q", cfg.EndpointUrl)
	}
}

func TestParseAppInsightsConnectionStringErrors(t *testing.T) {
	testCases := []struct {
		name        string
		value       string
		wantErrText string
	}{
		{
			name:        "empty",
			value:       "",
			wantErrText: "connection string is empty",
		},
		{
			name:        "missing instrumentation key",
			value:       "IngestionEndpoint=https://westus3-1.in.applicationinsights.azure.com/",
			wantErrText: "instrumentation key is missing",
		},
		{
			name:        "missing ingestion endpoint",
			value:       "InstrumentationKey=test-key",
			wantErrText: "ingestion endpoint is missing",
		},
		{
			name:        "bad ingestion endpoint",
			value:       "InstrumentationKey=test-key;IngestionEndpoint=:bad://",
			wantErrText: "missing protocol scheme",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseAppInsightsConnectionString(tc.value)
			if err == nil {
				t.Fatalf("expected error containing %q", tc.wantErrText)
			}
			if !strings.Contains(err.Error(), tc.wantErrText) {
				t.Fatalf("expected error containing %q, got %q", tc.wantErrText, err.Error())
			}
		})
	}
}
