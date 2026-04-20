Careme is your personal chef and sommilier. It will

1. Take your favorite grocery store based on location
2. Check the stores inventory for fresh meat and seasonal produce
3. Generate a weekly meal plan from a variety of cuisines and cooking styles.


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
- `AZURE_MONITOR_OTLP_TRACES_ENDPOINT` - Azure Monitor OTLP HTTPS traces endpoint from the Application Insights "OTLP Connection Info" page
- `AZURE_MONITOR_OTLP_LOGS_ENDPOINT` - Azure Monitor OTLP HTTPS logs endpoint from the Application Insights "OTLP Connection Info" page
- `OTEL_EXPORTER_OTLP_ENDPOINT` - generic OTLP HTTP endpoint for non-Azure or local collector setups
- `OTEL_SERVICE_NAME` - optional override for the reported OpenTelemetry service name (defaults to the current binary name)
- `SENDGRID_API_KEY` - To allow sending weekly recipe lists via email
- `ALBERTSONS_SEARCH_SUBSCRIPTION_KEY` - Albertsons-family pathway search subscription key
- `ALBERTSONS_SEARCH_REESE84` - fallback Albertsons-family `reese84` cookie when cache is empty or stale
- `BRIGHTDATA_BROWSER_WS_ENDPOINT` - Bright Data Browser API websocket endpoint for `cmd/albertsonsreese84`; may include embedded credentials
- `AZURE_STORAGE_ACCOUNT_NAME` and `AZURE_STORAGE_PRIMARY_ACCOUNT_KEY` - enable Azure Blob-backed cache storage

Direct Azure OTLP export also requires a Microsoft Entra credential at runtime. In Kubernetes that means workload identity or managed identity, or standard Azure SDK env vars such as `AZURE_CLIENT_ID`, `AZURE_TENANT_ID`, and `AZURE_CLIENT_SECRET`.

if you're
- `ENABLE_MOCKS` - For testing if you have none of the above

## Development
See agents.md for some more but 
go test ./... on any go change 
and 
```
bash tailwind/generate.sh
```
if you change input css or any *.html

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
