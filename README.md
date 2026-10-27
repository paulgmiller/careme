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
- `KROGER_CART_TOKEN_KEY` - base64-encoded 32-byte encryption key enabling Kroger cart transfer. Register `${PUBLIC_ORIGIN}/kroger/callback` as a redirect URI with Kroger and grant `cart.basic:write`; keep this key stable so existing connections remain usable.
- `OPENROUTER_API_KEY` - OpenRouter API key for cached recipe critique generation
- `OPENROUTER_CRITIQUE_MODEL` - OpenRouter model slug for recipe critique (defaults to `google/gemini-4-argon`)
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

## Adding a provider

Add a store integration in this sequence: **locations → staples → optional
ordering**. These are separate interfaces so location discovery can work before
a catalog is available, and recipe generation can work without ordering. Keep
the implementations together in `internal/providers/<provider>` and share one
location-ID namespace and `IsID` implementation across them.

### 1. Add locations

Implement `locations.LocationBackend`: `IsID`, `GetLocationByID`,
`GetLocationsByCoordinates`, and `HasInventory`. Use stable location IDs and
return the store or delivery-area details needed by location selection. Keep
`HasInventory` false until the location has a supported recipe catalog.

Register the backend in `providerregistry.Factory.NewLocationBackends` in
`internal/providerregistry/locations.go`. Delivery providers can represent a
service area instead of physical stores; Smith Brothers Farms and MNFoodClub
are examples. Optionally implement `CachePolicy() (version string, cacheable bool)`
to version location cache entries or disable caching when results depend on the
search point. Backends without this method use the default unversioned cache.

### 2. Add staples

Implement `recipes.StaplesBackend`: `IsID`, `Signature`, `FetchStaples`, and
`FetchWines`. Staples supply the ingredient catalog used to generate recipes;
they do not add products to a cart. Return `ai.InputIngredient` entries with
stable product IDs, descriptions, and available size, price, and aisle details.
Wine lookup is part of the current interface; providers without it should return
an explicit unsupported error. `FetchPantry` is an optional additional interface.

Register the backend in `providerregistry.Factory.NewStaplesBackends` in
`internal/providerregistry/staples.go`, and its identity in
`internal/providerregistry/identity.go` so ingredient and recipe cache keys use
the provider's `Signature`. Change that signature when catalog definitions or
parsing rules require cached ingredients to be refreshed. Enable `HasInventory`
for supported locations once staples are available.

Update [cache layout](docs/cache-layout.md) if cache keys or prefixes change.

### 3. Optionally add ordering

Implement `POST /<destination>/cart-transfers` as a browser navigation using
`orders/types.TransferRequest`. The shopping list submits the serialized request
and a stable idempotency key in hidden form fields. The destination owns
authentication, matching, cart writes, duplicate prevention, redirects, and error
pages. It redirects directly to account connection when needed and to the
shopping list when the addition completes.

Register routes in `providerregistry.Factory.RegisterOrders`, then add the
location match and view builder through `Factory.NewOrderViewBuilder(cache)`.
The shopping-list UI
asks the provider for the current viewer's saved handoff during server rendering.
Confirmed or possibly applied writes show a cart link instead of the form;
transfer-read failures fail the page. There is no browser status fetch.
Kroger is currently the only destination; future integrations can adapt their
upstream APIs behind the same browser handoff. Users review amounts and complete
checkout with the provider. See [Cart transfers](docs/ordering.md) for browser
navigation and OAuth details.

### Verify the integration

Test location lookup and service-area filtering, consistent ID routing, catalog
request/response shapes, parsing failures, and cache signatures. For ordering,
also test matching gaps, submission failures, duplicate prevention or link reuse,
and rendered results. Use deterministic fixtures and explicit fake clients.

Run `./task.sh verify-go -- -cover`; run `./task.sh verify-ui` and inspect the UI
when templates change.

Run `./task.sh k8s-secrets` to check that every Deployment and CronJob using the
`careme` or `careme-ingredientreview` image imports its required secrets through
`envFrom`. Maintain the provider and full-config secret lists and image mapping
in `deploy/secrets_test.go` when configuration or deployed binaries change.
Secrets remain separate; the check reads manifests without accessing credentials
or Kubernetes. It also runs in `k8s-lint`, which gates publishing and deployment
in CI. It checks secret references, not the keys or values stored in the cluster.

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

Then open `http://127.0.0.1:8090/grader?location=70100023`, replacing the ID with the store you want to review. A location ID is required for viewing and submitting reviews.

The app reads the current store day’s cached ingredients, or the previous store day’s on a cache miss, matching the produce score. It shows graded items one at a time and records each as too high, correct, or too low. It skips ungraded items and never fetches or grades inventory. Missing catalogs return a not-found response. Each location’s catalog stays in memory for 30 minutes after loading, then is purged even when idle. The next request reloads it. Successfully reviewed items are removed from the active store’s in-memory catalog immediately; failed saves leave them available to retry. Items reviewed elsewhere are removed as the active store advances past them. Location lookup uses the normal location providers and their cache. The reviewer loads only provider configuration, requiring Kroger credentials (and credentials for any other enabled provider that needs them) plus cache access. Clerk and AI configuration are not required; the deployment mounts the `kroger` and `storage` secrets.

Reviews include the ingredient snapshot, excluding embeddings. Reviews are shared across stores by ingredient and displayed score; a changed score can be reviewed again.


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

Run `careme -campaigns` to generate recipes and images for advertised stores with `Generate: true` that are due during their local 9 AM hour. MN Food Club, TC Farm, and Nelson Grass Farm generate weekly on Monday in their local timezone (`America/Chicago`); other enabled stores generate daily at 9 AM local time. Smith Brothers Farms generation remains disabled, with its campaign redirect active. The job creates its own flex AI client and exits with an error if any due store fails. It reuses cached shopping lists and images, and retries incomplete work even when parameters were saved by an earlier attempt.

For the three MNFoodClub storefronts and Smith Brothers Farms, recipe requests throughout the week resolve to Monday's list, including explicit dates within that week. The week switches on Monday at the existing 9 AM local store-day boundary. Previously shared hash links keep their original lists.

`deploy/cronjob-careme-advertised-recipes.yaml` runs the application image directly on `ADVERTISED_RECIPES_SCHEDULE`, at the top of each hour from 6–10 AM Pacific in production (`0 6-10 * * *`, `America/Los_Angeles`), with the same store, AI, auth, storage, and telemetry credentials used by the mail job. These five daily starts cover 9 AM in the Eastern, Central, Mountain, and Pacific timezones, plus one hour afterward. Each store's timezone determines when it is due, including daylight saving changes; runs with no due stores exit without generating. Delayed starts and retries are eligible throughout the local 9 AM hour, with cached output preventing duplicate generation. The extra scheduled hours do not extend that eligibility window. It no longer calls the web server's generation endpoint. Kubernetes prevents overlapping runs and allows one job retry.

Automatic campaign runs are temporarily suspended in `caremetest` by `deploy/deploy.sh`; production still runs daily. To manually run generation-enabled advertised stores in test using the deployed image and credentials, create a Job from the suspended CronJob:

```sh
kubectl create job -n caremetest --from=cronjob/careme-advertised-recipes "careme-advertised-recipes-manual-$(date +%s)"
```

Manual Jobs do not change the suspension and are not protected by the CronJob's overlap prevention, so wait for an existing run to finish before starting another. To restore automatic test runs on the 1st and 15th of each month, set `advertised_recipes_suspend="false"` in the `caremetest` block of `deploy/deploy.sh` and deploy.

### MNFoodClub ingredients

Nearby location search includes three home-delivery storefronts sharing the
MNFoodClub catalog: MN Food Club (`mnfood.club`), TC Farm (`tc.farm`), and
Nelson Grass Farm (`nelsongrassfarm.com`). All three appear within an approximate
rectangle from the supplied coverage map: latitude 43.90–45.65 and longitude
-94.30–-92.35. This is a rough discovery area, not the exact delivery boundary.
Search results use stable IDs `mnfoodclub_delivery`, `tcfarm_delivery`,
and `nelsongrassfarm_delivery`, respectively. Each result retains the
search coordinates for distance filtering; lookup by ID uses the shared Blaine
coordinates for store dates. Location results are not cached.

Online delivery locations include their storefront URL, displayed instead of
the address in location search and as a link in shopping list and recipe headers.
Smith Brothers Farms uses the same display behavior. Previously saved recipe
parameters retain their original location details.

Fetch all visible, available, in-stock catalog products across every category,
plus the static weekly produce-share contents, and display their prices:

```sh
INGREDIENT_GRADING_ENABLE=false go run ./cmd/ingredients -location mnfoodclub_delivery -verbose
```

For weekly share updates, download the TC Farm Produce Weekly Feature PDF while
signed in to SharePoint, then extract its first page with Poppler's `pdftotext`:

```sh
pdftotext -f 1 -l 1 -layout '/path/to/TC Farm Produce Weekly Feature.pdf' -
```

Check the delivery dates and all six share lists, then update
`internal/providers/mnfoodclub/produce_shares.go` and its test expectations and run
`./task.sh verify-go`. The PDF also contains previous weeks and recipe kits;
only the current week's produce-share page supplies this inventory. Preserve
alternatives such as “Stripetti or Spaghetti Squash” as one item and include only
explicitly listed substitutions. Share-content changes automatically change the
provider's cache signature. Deploy the updated application to serve the new list.

A short-term automation can turn this extraction into a proposed weekly PR for
review. Unattended downloading needs authenticated SharePoint access or a public
download link; the supplied document URL currently rejects anonymous requests.

Set `MNFOODCLUB_API_PATH` to the supplied store API URL
(`https://api.bigcommerce.com/stores/<store_hash>/v3/`, including the trailing slash) and
`MNFOODCLUB_ACCESS_TOKEN` to the store access token. The token needs Products
read access; client ID, client secret, and account names are not used. These
environment variables can later be supplied through kage or Kubernetes secrets.
The local `secrets/bigcommerce` credential note is ignored by Git and is not loaded
automatically. To use that note locally without printing its credentials:

```sh
export MNFOODCLUB_API_PATH="$(sed -n 's/^API PATH[:=][[:space:]]*//p' secrets/bigcommerce)"
export MNFOODCLUB_ACCESS_TOKEN="$(sed -n 's/^ACCESS TOKEN[:=][[:space:]]*//p' secrets/bigcommerce)"
```

Any store ID starting with `mnfoodclub_` routes to the same BigCommerce catalog.
The provider fetches every page (up to 250 products per request), resolves brands
and category names, and uses the purchasable variant with the highest inventory
level for each product. Ties keep the first eligible variant in catalog order.
Zero stock excludes items only when inventory tracking is enabled. The selected
variant's prices and `variant_id` are saved alongside the parent product `id` in
cached ingredient data; the TSV includes only the parent product ID. Older
cached catalogs need refreshing to populate `variant_id`. The saved IDs support
the proposed [MN Food Club ordering API](docs/mnfoodclub-ordering.md).

For a test-site cart handoff walkthrough and standalone mock API, see
[MN Food Club mock walkthrough](docs/mnfoodclub-mock.md). The embedded demo is
available at `/mnfoodclub-demo/` only when `ENABLE_MOCKS` is enabled.
Prices are standard catalog prices, without custom membership discounts. Sizes
come from product names, not shipping weights or unverified custom fields.
Missing credentials fail client construction when the staples backends are
assembled. Failed API pages fail the import without partial results.
With grading disabled,
the displayed 10/10 scores are placeholders, not quality assessments. Enable
grading with `INGREDIENT_GRADING_ENABLE=true` and configure `AI_API_KEY` to use
the existing grading and nearest-ingredient lookup. Wine candidates include every
page of the Wine & Wine Alternatives category (BigCommerce category 178), using
the same stock and pricing rules. Requested wine styles do not restrict this list.

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

The provider fetches the public produce, meat/poultry, dairy/eggs, and yogurt pages
through the existing Bright Data client. The organic and standard produce-box products
are replaced with individual ingredients from the live bullet lists on their
product pages.
Each box ingredient includes its box name and a preassigned 10/10 produce-share
grade; individual quantities and prices are unspecified. Seasonal contents update
on the next uncached weekly ingredient fetch. Ingredient and recipe hashes use
Monday's date, switching weeks at 9 AM local time on Monday. No Smith Brothers
Farms account credentials are required. Wine lookup is unsupported.

### Ingredient embedding lookup

Ingredient grading and wine pairing default to `gpt-6-luna` with reasoning
disabled. Set `INGREDIENT_GRADING_MODEL=decisions` to use the OpenAI Decisions API
with `gpt-6-luna` instead of the former JEV integration. It uses the same ten-level
rubric, rounding its zero-based score and adding one for a 1–10 ingredient grade,
and requires `AI_API_KEY`. Each uncached ingredient gets its own Decisions
request, with up to 64 concurrent requests per grader instance. Explicit model names select the
Responses grader; recipe generation and meal planning continue to use `gpt-6.1-sol`.

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
