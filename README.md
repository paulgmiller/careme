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
  - Email and campaign recipe/menu generation requests use flex processing. Each email delivery and campaign location has a 10-minute budget covering generation and retries. Interactive requests retain their existing processing tier.
  - Images continue to use standard image generation; the Images API does not expose a flex service tier. Text spend logs include the returned service tier and apply flex rates when served on flex.
### Optional 
- `OPENROUTER_API_KEY` - OpenRouter API key for cached recipe critique generation
- `OPENROUTER_CRITIQUE_MODEL` - OpenRouter model slug for recipe critique (defaults to `google/gemini-3.1-pro-preview`)
- `CLARITY_PROJECT_ID` - Microsoft Clarity project ID for web analytics (optional)
- `GOOGLE_TAG_MANAGER_ID` - Google Tag Manager container ID for web analytics and ad conversion tags (optional); see `docs/gtm-ads.md` for conversion setup
- `OTEL_EXPORTER_OTLP_ENDPOINT` - OTLP HTTP endpoint. For Grafana Cloud, use the endpoint from the OpenTelemetry connection tile.
- `OTEL_EXPORTER_OTLP_HEADERS` - OTLP headers. For Grafana Cloud, use the generated `Authorization=Basic ...` header value from the OpenTelemetry connection tile.
- `SENDGRID_API_KEY` - To allow sending weekly recipe lists via email
- `ALBERTSONS_SEARCH_SUBSCRIPTION_KEY` - Albertsons-family pathway search subscription key
- `ALBERTSONS_SEARCH_REESE84` - fallback Albertsons-family `reese84` cookie when cache is empty or stale
- `BRIGHTDATA_BROWSER_WS_ENDPOINT` - Bright Data Browser API websocket endpoint for `cmd/reese84` and `cmd/publixabck`; may include embedded credentials
- `AZURE_STORAGE_ACCOUNT_NAME` and `AZURE_STORAGE_PRIMARY_ACCOUNT_KEY` - enable Azure Blob-backed cache storage

For Grafana Cloud, the direct OTLP setup uses standard upstream OpenTelemetry env vars. Grafana's docs provide generated values for `OTEL_EXPORTER_OTLP_ENDPOINT` and `OTEL_EXPORTER_OTLP_HEADERS`.

if you're
- `ENABLE_MOCKS` - For testing if you have none of the above

## Mail placement tests

Send today's real recipe email through the production sender to test Gmail placement:

```sh
go run ./cmd/mailtest -to you@gmail.com
```

This requires `SENDGRID_API_KEY` and an existing Careme profile for the address with a favorite store. It sends only to the requested address, ignoring the profile's opt-in, shopping day, and previous-send record without changing the next scheduled send.

## Ingredient grade review

Run the small local review app with:

```sh
go run ./cmd/ingredientreview
```

Then open `http://127.0.0.1:8090/grader`. It shows cached ingredient grades one at a time and records each as too high, correct, or too low.


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

### Advertised recipe cronjob

Run `careme -campaigns` to generate recipes and images for advertised stores with `Generate: true` once. Smith Brothers Farms and MNFoodClub generation is disabled for now while we consider a weekly schedule; their campaign redirects remain active. The job creates its own flex AI client and exits with an error if any enabled store fails. It reuses cached shopping lists and images, and retries incomplete work even when parameters were saved by an earlier attempt.

`deploy/cronjob-careme-advertised-recipes.yaml` runs the application image directly on `ADVERTISED_RECIPES_SCHEDULE`, with the same store, AI, auth, storage, and telemetry credentials used by the mail job. It no longer calls the web server's generation endpoint. Kubernetes prevents overlapping runs and allows one job retry.

Automatic campaign runs are temporarily suspended in `caremetest` by `deploy/deploy.sh`; production still runs daily. To manually run generation-enabled advertised stores in test using the deployed image and credentials, create a Job from the suspended CronJob:

```sh
kubectl create job -n caremetest --from=cronjob/careme-advertised-recipes "careme-advertised-recipes-manual-$(date +%s)"
```

Manual Jobs do not change the suspension and are not protected by the CronJob's overlap prevention, so wait for an existing run to finish before starting another. To restore automatic test runs on the 1st and 15th of each month, set `advertised_recipes_suspend="false"` in the `caremetest` block of `deploy/deploy.sh` and deploy.

### MNFoodClub ingredients

Nearby location search includes MNFoodClub home delivery within an approximate
rectangle from the supplied coverage map: latitude 43.90–45.65 and longitude
-94.30–-92.35. This is a rough discovery area, not the exact delivery boundary.
Search results use `mnfoodclub_<latitude>_<longitude>` IDs, with the delivery
coordinates retained for distance filtering and store dates.

Fetch the first three pages of both produce (sorted by bestselling) and meat,
and display their ingredients and prices:

```sh
INGREDIENT_GRADING_ENABLE=false go run ./cmd/ingredients -location mnfoodclub_delivery -verbose
```

Any store ID starting with `mnfoodclub_` routes to the same public catalog through
the staples provider. No MNFoodClub credentials are required. Variant price ranges
use the starting price. With grading disabled,
the displayed 10/10 scores are placeholders, not quality assessments. Enable
grading with `INGREDIENT_GRADING_ENABLE=true` and configure `AI_API_KEY` to use
the existing grading and nearest-ingredient lookup. This provider supplies no wine
candidates.

### Smith Brothers Farms ingredients

Nearby search includes Smith Brothers Farms home delivery in two approximate
urban-corridor boxes: Puget Sound (latitude 46.95–48.05, longitude -122.80–-121.90)
and Greater Portland (latitude 45.30–45.80, longitude -122.95–-122.35).
These are rough discovery areas from the [service-area map](https://www.smithbrothersfarms.com/our-service-area),
not exact delivery boundaries, and exclude some outer delivery routes.
Search results use the stable `smithbrothersfarms_delivery` ID at the current
search coordinates; delivery locations bypass the location cache. Lookup by ID
uses the Kent headquarters coordinates.

```sh
INGREDIENT_GRADING_ENABLE=false go run ./cmd/ingredients -location smithbrothersfarms_delivery -verbose
```

The provider fetches the public produce and meat/poultry pages through the existing
Bright Data client. The organic and standard produce-box products are replaced
with individual ingredients from the live bullet lists on their product pages.
Each box ingredient includes its box name and a preassigned 10/10 produce-share
grade; individual quantities and prices are unspecified. Seasonal contents update
on the next uncached store-day ingredient fetch. No Smith Brothers Farms account
credentials are required. Wine lookup is unsupported.

### Ingredient embedding lookup

Ingredient grading and wine pairing default to `gpt-6-luna` with reasoning
disabled. Set `INGREDIENT_GRADING_MODEL` to override the grader; recipe generation
and meal planning continue to use `gpt-6.1-sol`.

With `AI_API_KEY` configured and `INGREDIENT_GRADING_ENABLE=1`, grading stores an
OpenAI `text-embedding-3-small` embedding of each ingredient description alongside
its grade, using a separate embedding cache keyed by model, dimensions, and
description. Existing grades are reused when missing embeddings are filled in;
changing the grader does not invalidate embeddings.

Find the nearest ingredient in a store's current staple catalog:

```sh
INGREDIENT_GRADING_ENABLE=1 go run ./cmd/ingredients -location 70100023 -ingredient "broccoli"
```

Add `-limit 10` for more neighbors. Results are JSON with ingredient metadata,
grade, and cosine similarity, ordered from closest to furthest. The first lookup
may fetch and grade the store catalog; subsequent lookups reuse cached product
grades and embeddings. Store provider credentials are the same as for the
existing ingredients command. Recipe generation context is unchanged.
