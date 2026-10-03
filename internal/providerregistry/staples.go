package providerregistry

import (
	"fmt"
	"net/http"

	"careme/internal/brightdata"
	"careme/internal/farmersmarket"
	"careme/internal/heb"
	"careme/internal/locations"
	"careme/internal/providers/albertsons"
	"careme/internal/providers/aldi"
	"careme/internal/providers/kroger"
	"careme/internal/providers/mnfoodclub"
	"careme/internal/providers/publix"
	"careme/internal/providers/smithbrothersfarms"
	"careme/internal/providers/walmart"
	"careme/internal/providers/wholefoods"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// NewStaplesBackends assembles grocery integrations in routing order.
func (f Factory) NewStaplesBackends() ([]locations.StaplesBackend, error) {
	cfg := f.config
	// Should this be per request so proxies can vary per user?
	brightdataClient, err := brightdata.NewProxyAwareHTTPClient(cfg.BrightDataProxy)
	if err != nil {
		return nil, fmt.Errorf("create bright data proxy-aware client: %w", err)
	}
	brightdataClient.Transport = otelhttp.NewTransport(brightdataClient.Transport)

	albertsonsProvider, err := albertsons.NewStaplesProvider(cfg.Albertsons, brightdataClient)
	if err != nil {
		return nil, fmt.Errorf("create albertsons staples provider: %w", err)
	}
	publixProvider, err := publix.NewStaplesProvider(cfg.Publix, brightdataClient)
	if err != nil {
		return nil, fmt.Errorf("create publix staples provider: %w", err)
	}
	hebProvider, err := heb.NewStaplesProvider(brightdataClient)
	if err != nil {
		return nil, fmt.Errorf("create heb staples provider: %w", err)
	}
	aldiProvider, err := aldi.NewStaplesProvider(brightdataClient)
	if err != nil {
		return nil, fmt.Errorf("create ALDI staples provider: %w", err)
	}

	// Kroger uses its public API, without the Bright Data proxy.
	httpClient := &http.Client{Transport: otelhttp.NewTransport(http.DefaultTransport)}
	krogerBackend, err := kroger.NewStaplesProvider(cfg, httpClient)
	if err != nil {
		return nil, fmt.Errorf("create kroger staples provider: %w", err)
	}
	farmersMarketProvider, err := farmersmarket.NewStaplesProvider()
	if err != nil {
		return nil, fmt.Errorf("create farmers market staples provider: %w", err)
	}

	return []locations.StaplesBackend{
		albertsonsProvider,
		hebProvider,
		aldiProvider,
		krogerBackend,
		publixProvider,
		farmersMarketProvider,
		mnfoodclub.NewStaplesProvider(mnfoodclub.NewClient(brightdataClient)),
		smithbrothersfarms.NewStaplesProvider(smithbrothersfarms.NewClient(brightdataClient)),
		walmart.NewStaplesProvider(),
		wholefoods.NewStaplesProvider(wholefoods.NewClient(brightdataClient)),
	}, nil
}
