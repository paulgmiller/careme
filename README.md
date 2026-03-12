Careme helps a signed-in user pick a grocery store, inspect store inventory and seasonality, and generate a weekly meal plan plus shopping list.

## Start Here

If you are trying to understand the repo quickly, read these in order:

1. `README.md` for the top-level map.
2. `AGENTS.md` for repo working rules.
3. `docs/data-object-flow.md` for recipe-generation state and cache-backed lifecycle.
4. `docs/cache-layout.md` for cache keys and persistence layout.
5. `docs/tour.md` for the main user journey.

## System Map

### Entry points
- `cmd/careme`: main app binary; serves the web UI and can run one-shot mail mode.
- `cmd/producecheck`: manual tool for produce scoring changes.
- `cmd/aldi`, `cmd/albertsons`, `cmd/publix`, `cmd/wholefoods`, `cmd/walmartstores`, `cmd/zipstorecount`: store and location helper tools.

### Core domains
- `internal/recipes`: recipe generation, regeneration, shopping list assembly, recipe handlers, and generation persistence.
- `internal/locations`: ZIP lookup, nearby-store selection, and the cross-store location abstraction.
- `internal/users`: user profile storage, favorite store, preferences, and admin user views.
- `internal/auth`: auth provider wiring; mostly Clerk-backed.
- `internal/ai`: provider glue for recipe generation.
- `internal/cache`: cache backends used as the main persistence layer.

### Store integrations
- `internal/kroger`: Kroger API client and types.
- `internal/aldi`
- `internal/albertsons`
- `internal/publix`
- `internal/walmart`
- `internal/wholefoods`

### Web UI
- `internal/templates`: server-rendered HTML templates.
- `internal/static`: compiled Tailwind and static assets.
- `cmd/careme/web.go`: top-level HTTP wiring.

## Main Runtime Flow

The critical user flow is:

1. User signs in and chooses a store.
2. `internal/locations` resolves the location and store metadata.
3. `internal/recipes` parses request params and saves them under a params hash.
4. The recipe generator builds a shopping list and recipe set.
5. Generated recipes, shopping list, and later save/dismiss actions are persisted through the cache layer.
6. Regeneration creates a new params object from prior params plus user feedback/selection state.

See `docs/data-object-flow.md` for the concrete object and cache-key lifecycle.

## Common Commands

### Safe Go cache setup

```sh
export GOCACHE=/tmp/go-build
export GOMODCACHE=/tmp/go-modcache
```

### Verify Go changes

```sh
go fmt ./...
go vet ./...
golangci-lint run ./...
ENABLE_MOCKS=1 go test ./...
```

### Run the app locally

```sh
go run ./cmd/careme -serve -addr :8080
```

### Rebuild CSS after template or CSS changes

```sh
bash tailwind/generate.sh
```

### Helpful targeted commands

```sh
go run ./cmd/careme -zipcode 98101
go build -o bin/careme ./cmd/careme
```

If you change the generator produce filter list in `internal/recipes/params.go`, also run:

```sh
go run ./cmd/producecheck -location 70500874
```

That command may require secrets from `.envtest`.

## Configuration

### Required
- `KROGER_CLIENT_ID`
- `KROGER_CLIENT_SECRET`
- `AI_API_KEY`

### Optional
- `CLARITY_PROJECT_ID`
- `GOOGLE_TAG_ID`
- `GOOGLE_CONVERSION_LABEL`
- `SENDGRID_API_KEY`
- `HISTORY_PATH`
- `AZURE_STORAGE_ACCOUNT_NAME`
- `AZURE_STORAGE_PRIMARY_ACCOUNT_KEY`

### Local test convenience
- `ENABLE_MOCKS=1` lets tests run without Kroger or AI credentials.

## Working Conventions

- Prefer server-rendered HTML and HTMX over SPA patterns.
- Keep custom JavaScript small and browser-specific.
- Prefer standard library and small focused packages.
- Keep generated artifacts and user recipe output out of commits unless intentionally adding fixtures.
- Any handler that exposes multi-user data belongs behind `/admin`.

## Docs Index

- `docs/data-object-flow.md`: recipe generation lifecycle.
- `docs/cache-layout.md`: authoritative cache key and prefix layout.
- `docs/htmx-migration-plan.md`: frontend migration constraints.
- `docs/produce-score-tradeoffs.md`: produce scoring notes.
- `docs/tour.md`: user-facing screen tour.

## Live Site

- Uptime: https://stats.uptimerobot.com/ehEFlvlNM9
- Infra note: Cloudflare fronts DNS and HTTPS.
