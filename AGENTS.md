# Repository Guidelines

## Project Structure & Module Organization
- `cmd/careme`: Entry point; `main.go` parses flags for CLI vs `-serve` web mode; `web.go` wires handlers and middleware.
- `internal/recipes`, `internal/locations`, `internal/kroger`: Business logic for meal planning, location lookup, and Kroger API access; generated client files live under `internal/kroger`.
- `internal/templates` and `cmd/careme/favicon.png`: HTML templates and assets for the UI; `internal/html` holds helpers (e.g., Clarity snippet).
- `internal/cache`, `internal/logsink`, `internal/ai`, `internal/users`: Cross-cutting services (caching, logging, AI provider glue, user storage).
- `recipes/`: Local output directory created at runtime; keep it out of commits unless intentionally adding fixtures.

## Build, Test, and Development Commands
- `go fmt ./...` then `go vet ./...`: Baseline formatting and static checks.
- `go test ./...`: Run unit tests across all packages; add `-cover` when changing core logic.
- `go run ./cmd/careme -serve -addr :8080`: Start the web server (requires env vars below).
- `go run ./cmd/careme -zipcode 98101`: Helper to list Kroger location IDs by ZIP.
- `go build -o bin/careme ./cmd/careme`: Produce a local binary for manual runs.

## Coding Style & Naming Conventions
- Go 1.24; keep code `gofmt`-clean before review. Favor small, focused functions and table-driven tests.
- Exported identifiers in `CamelCase`; package-private helpers in `lowerCamel`. Template names mirror file names in `internal/templates`.
- Prefer standard library first; add dependencies sparingly and record rationale in PR description if new.

## Testing Guidelines
- Place tests alongside code in `*_test.go`; prefer table-driven cases and explicit fixtures over implicit globals.
- Use `go test ./... -run TestName` for targeted debugging; keep deterministic by avoiding network calls and using fakes where possible.
- When touching recipe generation or Kroger client code, add assertions that cover API shape changes and template output (see existing tests in `internal/recipes` and `internal/html`).

## Commit & Pull Request Guidelines
- Follow the existing history: short, imperative summaries (e.g., “Fix Kroger location parsing”). Reference an issue/PR number when applicable.
- In PRs, include: what changed, why, how to verify (commands run), and any config/env impacts. Add screenshots for UI changes using `internal/templates`.
- Keep commits scoped and reviewable; avoid mixing refactors with feature changes unless necessary.

## Security & Configuration Notes
- Required env vars: `KROGER_CLIENT_ID`, `KROGER_CLIENT_SECRET`, `AI_API_KEY`; optional `AI_PROVIDER`, `AI_MODEL`, `CLARITY_PROJECT_ID`, `HISTORY_PATH`. Azure logging uses `AZURE_STORAGE_ACCOUNT_NAME` and `AZURE_STORAGE_PRIMARY_ACCOUNT_KEY`.
- Never commit secrets or generated recipe outputs. If testing against real APIs, use minimal scopes and rotate keys promptly.
