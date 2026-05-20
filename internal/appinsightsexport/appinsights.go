package appinsightsexport

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	msappinsights "github.com/microsoft/ApplicationInsights-Go/appinsights"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"
)

const (
	ConnectionStringEnv      = "APPLICATIONINSIGHTS_CONNECTION_STRING"
	appInsightsTrackPath     = "/v2/track"
	appInsightsBatchSize     = 1024
	appInsightsBatchInterval = 10 * time.Second
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

type Config struct {
	InstrumentationKey string
	IngestionEndpoint  *url.URL
	Client             *http.Client
}

func Enabled() bool {
	return strings.TrimSpace(os.Getenv(ConnectionStringEnv)) != ""
}

func ParseConnectionString(connectionString string) (*Config, error) {
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
	return &Config{
		InstrumentationKey: instrumentationKey,
		IngestionEndpoint:  u,
	}, nil
}

func LoadConfig() (*Config, error) {
	return ParseConnectionString(os.Getenv(ConnectionStringEnv))
}

func newAppInsightsTelemetryClient(cfg *Config) (msappinsights.TelemetryClient, error) {
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
	return client, nil
}

func applyAppInsightsClientContext(client msappinsights.TelemetryClient, res *resource.Resource, serviceVersion string) {
	if client == nil {
		return
	}
	ctx := client.Context()
	if ctx == nil {
		return
	}

	ctx.Tags.Cloud().SetRole(resourceString(res, attrServiceName, "careme"))
	if version := resourceString(res, attrServiceVersion, serviceVersion); version != "" {
		ctx.CommonProperties[attrServiceVersion] = version
	}
	if service := resourceString(res, attrServiceName, "careme"); service != "" {
		ctx.CommonProperties[attrServiceName] = service
	}
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
