Careme is your personal chef and sommilier. It will

1. Take your favorite grocery store based on location
2. Check the stores inventory for fresh meat and seasonal produce
3. Generate a weekly meal plan from a variety of cuisines and cooking styles.

Learn way more at https://careme.cooking/about or go generate a recipe https://careme.cooking. 

## Development


![Go](https://img.shields.io/badge/Go-1.26-blue)
[![License: BUSL-1.1](https://img.shields.io/badge/License-BUSL--1.1-blue.svg)](https://github.com/paulgmiller/careme/blob/master/LICENSE)
![Last Commit](https://img.shields.io/github/last-commit/paulgmiller/careme)
[![CI](https://github.com/paulgmiller/careme/actions/workflows/go.yml/badge.svg)](https://github.com/paulgmiller/careme/actions/workflows/go.yml)
[![Ask DeepWiki](https://deepwiki.com/badge.svg)](https://deepwiki.com/paulgmiller/careme)

See agents.md for some more but 
go test ./... on any go change 
and 
```
bash tailwind/generate.sh
```
if you change input css or any *.html


## Configuration

The application is configured via environment variables:
### Mandatory 
- `KROGER_CLIENT_ID` - Kroger API client ID (required)
- `KROGER_CLIENT_SECRET` - Kroger API client secret (required)
- `AI_API_KEY` - OpenAI API key for recipe generation and chat (required)
### Optional 
- `GEMINI_API_KEY` - Gemini API key for cached recipe critique generation
- `GEMINI_CRITIQUE_MODEL` - Gemini model for recipe critique (defaults to `gemini-2.5-flash`)
- `CLARITY_PROJECT_ID` - Microsoft Clarity project ID for web analytics (optional)
- `GOOGLE_TAG_ID` - Google Ads/gtag ID for web analytics (optional)
- `GOOGLE_CONVERSION_LABEL` - Google Ads conversion label used on `/auth/establish?signup=true` (optional)
- `OTEL_EXPORTER_OTLP_ENDPOINT` - OTLP HTTP endpoint. For Grafana Cloud, use the endpoint from the OpenTelemetry connection tile.
- `OTEL_EXPORTER_OTLP_HEADERS` - OTLP headers. For Grafana Cloud, use the generated `Authorization=Basic ...` header value from the OpenTelemetry connection tile.
- `SENDGRID_API_KEY` - To allow sending weekly recipe lists via email
- `ALBERTSONS_SEARCH_SUBSCRIPTION_KEY` - Albertsons-family pathway search subscription key
- `ALBERTSONS_SEARCH_REESE84` - fallback Albertsons-family `reese84` cookie when cache is empty or stale
- `BRIGHTDATA_BROWSER_WS_ENDPOINT` - Bright Data Browser API websocket endpoint for `cmd/albertsonsreese84` and `cmd/publixabck`; may include embedded credentials
- `AZURE_STORAGE_ACCOUNT_NAME` and `AZURE_STORAGE_PRIMARY_ACCOUNT_KEY` - enable Azure Blob-backed cache storage

For Grafana Cloud, the direct OTLP setup uses standard upstream OpenTelemetry env vars. Grafana's docs provide generated values for `OTEL_EXPORTER_OTLP_ENDPOINT` and `OTEL_EXPORTER_OTLP_HEADERS`.

if you're
- `ENABLE_MOCKS` - For testing if you have none of the above


## Cache Key Layout
See [docs/cache-layout.md](docs/cache-layout.md) for the authoritative cache key/prefix layout and backend notes.

## Frontend Approach
- Prefer server-rendered HTML and HTMX for interactive behavior.
- Avoid SPA-style architecture for routine page interactions.
- Keep custom JavaScript minimal and focused on browser-only APIs.
- Migration plan: [docs/htmx-migration-plan.md](docs/htmx-migration-plan.md)

## Live site

* Uptime https://stats.uptimerobot.com/ehEFlvlNM9
* Cloudflare for dns and https proxying
