# Repository Guidelines

## Read First

Use the repo docs as the source of truth before inferring behavior from scattered handlers.

- `README.md`: top-level system map and common commands.
- `docs/data-object-flow.md`: recipe generation lifecycle and object transitions.
- `docs/cache-layout.md`: cache key and prefix layout. Update this doc when cache layout changes.
- `docs/tour.md`: main user journey and UI context.
- `internal/locations/README.md`: provenance and generation notes for ZIP centroid data.

Assume `Taskfile.yml` is the canonical entrypoint for routine local work. Prefer reading it before inventing new command sequences.

## Project Structure

- `cmd/careme`: main binary. `main.go` handles startup and `web.go` wires HTTP routes, services, and shutdown.
- `internal/recipes`: recipe generation, regeneration, shopping list assembly, handlers, and persistence helpers.
- `internal/locations`: store lookup, ZIP centroid logic, and nearby-store abstractions.
- `internal/users`: user storage, profile endpoints, favorites, preferences, and admin user views.
- `internal/auth`: auth provider integration; mostly Clerk authorization.
- `internal/cache`: backing storage abstraction used across the app.
- `internal/ai`: AI provider client and recipe-generation glue.
- `internal/templates`, `internal/static`: server-rendered UI templates and static assets.
- `internal/<store>` packages: store-specific clients and data collection logic.
- `recipes/`: runtime output directory. Do not commit generated recipe output unless intentionally adding fixtures.

## Working Style

- Go 1.24. Keep code `gofmt`-clean.
- Prefer small functions, explicit control flow, and table-driven tests.
- Prefer standard library first; add dependencies sparingly.
- Prefer simple HTML and HTMX over heavier frontend frameworks.
- For UI copy, use plain culinary language. Example: "Try again, chef" instead of "Regenerate".
- Keep comments brief and only where intent is not obvious from code.

## Development Commands

Recommended for sandbox-safe Go work:

```sh
export GOCACHE=/tmp/go-build
export GOMODCACHE=/tmp/go-modcache
```

Install Task if needed:

```sh
go install github.com/go-task/task/v3/cmd/task@latest
```

Baseline verification:

```sh
task verify
```

Useful local runs:

```sh
task serve
task css
task zipcode ZIP=98101
```

When changing the generator produce filter list in `internal/recipes/params.go` `Produce()`, also run:

```sh
task producecheck LOCATION=70500874
```

That command may require secrets from `.envtest`.

If a task target exists for the workflow you need, use it instead of retyping the underlying commands. If `task` is unavailable in a restricted environment, inspect `Taskfile.yml` and run the equivalent commands directly for that session.

## Testing Expectations

- After normal code changes, run `task verify`.
- For quick iteration, it is fine to run a narrower command or task first, but finish with `task verify` unless the change is clearly docs-only.
- Keep tests deterministic. Avoid network calls and prefer fakes or mocks.
- Place tests next to code in `*_test.go`.
- When touching recipe generation or Kroger client code, add assertions for API-shape drift and rendered output where applicable.
- If you cannot run tests or lint, say so explicitly.

## Change-Specific Rules

- If you change cache keys or cache-backed object layout, update `docs/cache-layout.md`.
- If you change recipe-generation state transitions, update `docs/data-object-flow.md`.
- If you change templates or input CSS, run `task css`.
- If you add a handler that can expose multi-user data, place it behind the `/admin` mux.

## Security And Configuration

Required env vars:
- `KROGER_CLIENT_ID`
- `KROGER_CLIENT_SECRET`
- `AI_API_KEY`

Optional env vars:
- `CLARITY_PROJECT_ID`
- `GOOGLE_TAG_ID`
- `GOOGLE_CONVERSION_LABEL`
- `HISTORY_PATH`
- `AZURE_STORAGE_ACCOUNT_NAME`
- `AZURE_STORAGE_PRIMARY_ACCOUNT_KEY`

Never commit secrets or incidental generated output. If testing against real APIs, use minimal scopes and rotate keys promptly.
