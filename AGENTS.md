# Repository Guidelines

## Working Agreement

- Complete authorized work through verification; make routine choices from existing code and user intent. Ask only about material ambiguity or missing authorization, and continue independent work while waiting.
- Keep changes scoped and preserve user edits. Delegate bounded, independent work when permitted and useful; handle small changes locally.
- Report the outcome, verification, and concrete blockers concisely. Do not repeat passing checks without new changes or unresolved risks.

## Project Map

- `cmd/careme`: CLI entry point (`main.go`), web handlers and middleware (`web.go`).
- `internal/recipes`, `internal/locations`, `internal/kroger`: Meal planning, location lookup, and Kroger access, including generated clients.
- `internal/templates`, `internal/static`, `internal/html`: UI templates, assets, and HTML helpers.
- `internal/cache`, `internal/logsink`, `internal/ai`, `internal/users`, `internal/auth`: Shared services and Clerk authentication/authorization.
- Configuration and environment variables: [README.md](README.md). Use the Go version in `go.mod` and normal shared Go caches.

## Verification

Run tasks from the repository root; `./task.sh --list` lists development commands. [Taskfile.yml](Taskfile.yml) owns command details.

- Go changes: `./task.sh verify-go`; for core logic use `./task.sh verify-go -- -cover`. For targeted iteration: `./task.sh test -- -run TestName`.
- HTML/CSS changes: `./task.sh verify-ui`, then inspect the affected UI and include screenshots in PRs. CSS generation requires Docker.
- Documentation only: review accuracy and run `git diff --check`.
- Changes to `internal/recipes/params.go` `Produce()`: run `./task.sh producecheck` before and after; report the score difference. Requires real API credentials from `.envtest`. If unavailable, report the check as blocked and complete other verification.
- Add tests for changed behavior, failure paths, and regressions, not implementation details. Recipe generation or Kroger changes need assertions for affected API shapes and template output.
- Keep tests alongside code in `*_test.go`; prefer table-driven cases, Testify assertions, deterministic fixtures, and explicit fakes/no-ops over nil dependencies unless testing nil behavior.
- Inspect the final diff for unintended edits. Report blocked checks with the command and reason; distinguish environment failures from regressions.

## Coding Conventions

- Prefer small functions and the standard library; justify new dependencies in PRs. Use `CamelCase` for exported identifiers and `lowerCamel` otherwise; template names mirror filenames.
- Prefer simple HTML and culinary UI copy: “Try again, chef” and “make it vegetarian.”
- Check callers before removing methods; exported methods used only by tests need no external compatibility protection.
- Pass constructor dependencies explicitly or use an options struct; do not fake optional arguments with variadic parameters.
- Required output must either succeed completely or return a contextual error. Do not silently omit components or add fallbacks/availability flags. Trust upstream invariants and remove redundant downstream checks. Best-effort behavior is for explicitly optional work, reflected in names, types, and tests.
- Update [docs/cache-layout.md](docs/cache-layout.md) when cache keys or prefixes change.

## Security and Handoff

- Handlers exposing multiple users’ data must be behind the `/admin` mux.
- Never print or commit secrets or commit generated recipe outputs. Keep runtime `recipes/` files out of commits unless intentionally adding fixtures. Use minimal scopes for real API testing and rotate keys promptly.
- Keep commits scoped. PRs explain what changed and why, verification commands, config/environment impacts, and relevant issue/PR numbers.
