# Repository Guidelines

## Project Structure & Module Organization
Careme is a Go service whose entry point lives in `cmd/careme`. `main.go` wires HTTP routing, middleware, and HTML templates. Core business logic is split across `internal` packages: `ai` for model orchestration, `kroger` for API clients (regenerated via oapi-codegen), `recipes` for meal planning, `templates` for UI fragments, and `users` for account state. Tests reside alongside their packages as `*_test.go` files. Deployment manifests and container assets are stored in `deploy/` and the top-level `Dockerfile`.

## Build, Test, and Development Commands
- `go run ./cmd/careme` — launch the local server using current configuration values.
- `go build ./cmd/careme` — compile the binary; use this to catch build-time errors before committing.
- `go test ./...` — execute the full test suite. Add `-cover` when you need a quick coverage snapshot.
- `go generate ./internal/kroger` — refresh generated client code after updating `swagger.yaml` or `cfg.yaml`.
- `docker build -t careme .` — produce an image matching the CI workflow.

## Coding Style & Naming Conventions
Follow standard Go style: run `gofmt` (or `go fmt ./...`) before pushing, keep indentation at tabs rendered as two spaces, and favor short, descriptive names in `lowerCamelCase` for locals and `CamelCase` for exported symbols. Group related helpers inside the appropriate `internal/<domain>` package, and keep HTML in `internal/templates` with snake_case file names to match existing templates.

## Testing Guidelines
Tests use Go’s built-in `testing` package. Mirror production package names and suffix files with `_test.go` (e.g., `internal/recipes/html_test.go`). Table-driven tests are preferred for validating recipe generation and template rendering. Strive to exercise new request handlers and cache paths, and avoid relying on live Kroger APIs—mock through in-memory fakes or fixtures.

## Commit & Pull Request Guidelines
Recent history favors concise, present-tense summaries (e.g., “improve error logging”). Keep commits scoped to a single concern and include configuration or schema updates they depend on. Pull requests should describe the change, outline test coverage (`go test ./...` output or screenshots for UI updates), and link any related issues or deployment notes. Request a review when the branch builds cleanly in CI.

## Configuration & Secrets
Runtime configuration is driven by environment variables defined in `README.md`: `KROGER_CLIENT_ID`, `KROGER_CLIENT_SECRET`, `AI_API_KEY`, provider/model overrides, and optional telemetry fields. Never commit `.env` or real credentials; when sharing examples, redact secrets (`export AI_API_KEY=sk-***`). For local experiments, use separate storage paths via `HISTORY_PATH` to avoid polluting shared recipe history.
