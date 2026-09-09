# Repository Guidelines

## Working Agreement

- Carry requested work through implementation, relevant verification, and a concise handoff. For an implementation request, a plan alone is not completion.
- Infer routine implementation choices from existing code and the user's intent. Ask only when missing information materially changes the outcome or a consequential action lacks authorization; continue independent work while awaiting an answer.
- Follow the user's current scope and retain earlier requirements when new messages refine the task. Avoid unrelated refactors and preserve existing user changes.
- Read the relevant code, nearby tests, and any more specific `AGENTS.md` before editing. Use `rg` for focused discovery; expand the search when evidence requires it.
- Treat these guidelines as repository defaults, subject to higher-priority instructions and explicit user direction. Do not invent approval requirements from stylistic preferences. Honor sandbox and tool permission boundaries.
- When delegation is available and permitted, use subagents for bounded, independent tasks only when they improve speed or confidence. Give each a clear scope and avoid overlapping edits; the primary agent integrates and verifies the result. Handle small, cohesive changes locally.
- Report meaningful findings and blockers briefly. The final response should state what changed, what verification passed or could not run, and any remaining limitation. Use plain language and file links; avoid recounting every tool call.

## Project Structure & Module Organization

- `cmd/careme`: Entry point; `main.go` parses flags for CLI vs `-serve` web mode; `web.go` wires handlers and middleware.
- `internal/recipes`, `internal/locations`, `internal/kroger`: Business logic for meal planning, location lookup, and Kroger API access; generated client files live under `internal/kroger`.
- `internal/templates` and `cmd/careme/favicon.png`: HTML templates and assets for the UI; `internal/html` holds helpers (e.g., Clarity snippet).
- `internal/cache`, `internal/logsink`, `internal/ai`, `internal/users`: Cross-cutting services (caching, logging, AI provider glue, user storage).
- `recipes/`: Local output directory created at runtime; keep it out of commits unless intentionally adding fixtures.
- `internal/auth`: Clerk authentication and authorization.

## Cache Layout

- Cache key/prefix docs live in `docs/cache-layout.md`. Keep that file updated when cache keys are added or changed.

## Build, Test, and Development Commands

- Go commands use the developer's normal shared Go caches. Codex permission
  profiles grant agents write access to those caches.
- `./task.sh fmt` (preferred), then `go vet ./...`: Baseline formatting and static checks.
- From the repo root, run `./task.sh lint`: Expanded Go linters using the pinned release binary.
- `export ENABLE_MOCKS=1`: Use mock Kroger and AI services for local testing without their credentials.
- `go test ./...`: Run unit tests across all packages; add `-cover` when changing core logic.
- `go run ./cmd/careme -serve -addr :8080`: Start the web server (requires env vars below).
- `go run ./cmd/careme -zipcode 98101`: Helper to list Kroger location IDs by ZIP.
- `go build -o bin/careme ./cmd/careme`: Produce a local binary for manual runs.
- `./tailwind/generate.sh`: Run from the repository root whenever CSS or HTML changes; requires Docker and updates `internal/static/tailwind.css`.

## Coding Style & Naming Conventions

- Use the Go version declared in `go.mod`. Always format Go changes with `./task.sh fmt`, and keep code `gofumpt`-clean before review. Favor small, focused functions and table-driven tests.
- Exported identifiers in `CamelCase`; package-private helpers in `lowerCamel`. Template names mirror file names in `internal/templates`.
- Prefer standard library first; add dependencies sparingly and record rationale in PR description if new.
- For tests, prefer `testify/assert` or `testify/require` to limit verbosity.
- Prefer simple HTML to JavaScript frameworks.
- For UI copy, prefer plain culinary language over technical terms (example: use "Try again, chef" instead of "Regenerate", and "make it vegetarian" instead of "prefer vegetarian").
- Nothing is used outside this repository. An exported method used only in tests may be removed when appropriate to the task; check callers first.
- Do not use variadic parameters to fake optional constructor arguments. Pass dependencies explicitly, or introduce a config/options struct when a constructor needs several optional settings.
- Prefer a single, strong success contract over partial-success plumbing. If an output component is required, return a contextual error when it cannot be produced; do not silently omit it, substitute a fallback, or add availability flags unless partial success is an explicit product requirement.
- Once an upstream function guarantees an invariant, let downstream code assume it. Remove redundant presence maps, booleans, nil checks, conditional template branches, and fallback paths.
- Reserve best-effort behavior for explicitly optional work, and make that optionality clear in names, types, and tests.

## Testing Guidelines

- After Go changes, run `./task.sh fmt`, `go vet ./...`, `go test ./...`, and `./task.sh lint` from the repository root. Use targeted tests during iteration; add `-cover` when changing core logic.
- After HTML or CSS changes, regenerate Tailwind CSS, run `go test ./...`, and inspect the affected UI. Include screenshots for template UI changes in PRs.
- For documentation-only changes, review accuracy and run `git diff --check`; Go tests, lint, and asset generation are unnecessary.
- Add or update tests for changed behavior, failure paths, and regressions. Avoid tests that merely repeat implementation details or test non-executable documentation.
- Once required checks pass, repeat or broaden them only for subsequent changes, failures, or unresolved risks. Before handing off, inspect the diff for unintended edits and generated outputs.
- If a required check is blocked, report the command and concrete reason. Distinguish environment failures from regressions; never describe an unrun check as passing.
- Place tests alongside code in `*_test.go`; prefer table-driven cases and explicit fixtures over implicit globals.
- Use `go test ./... -run TestName` for targeted debugging; keep deterministic by avoiding network calls and using fakes where possible.
- Prefer explicit fakes or no-op implementations over passing nil dependencies in tests, unless nil behavior is the thing under test.
- When touching recipe generation or Kroger client code, add assertions that cover API shape changes and template output (see existing tests in `internal/recipes` and `internal/html`).
- When changing the generator produce filter list (`internal/recipes/params.go` `Produce()`), also run `go run ./cmd/producecheck -location 70500874` before and after the change and report the score difference. This requires real API credentials from `.envtest`; do not print or commit them. If credentials are unavailable, report the check as blocked and complete other verification.

## Commit & Pull Request Guidelines

- Reference an issue/PR number when applicable. Say why something was done rather than just what was done.
- In PRs, include: what changed, why, how to verify (commands run), and any config/env impacts. Add screenshots for UI changes using `internal/templates`.
- Keep commits scoped and reviewable; avoid mixing refactors with feature changes unless necessary.

## Security & Configuration Notes

- Required env vars: `KROGER_CLIENT_ID`, `KROGER_CLIENT_SECRET`, `AI_API_KEY`; optional `OPENROUTER_API_KEY`, `OPENROUTER_CRITIQUE_MODEL`, `CLARITY_PROJECT_ID`, `GOOGLE_TAG_MANAGER_ID`, `OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_EXPORTER_OTLP_HEADERS`. Azure Blob cache still uses `AZURE_STORAGE_ACCOUNT_NAME` and `AZURE_STORAGE_PRIMARY_ACCOUNT_KEY`. Grafana Cloud direct OTLP uses the standard upstream OpenTelemetry endpoint and headers env vars.
- Never commit secrets or generated recipe outputs. If testing against real APIs, use minimal scopes and rotate keys promptly.
- Any handler that exposes data from multiple users must go behind the `/admin` mux to secure it.
