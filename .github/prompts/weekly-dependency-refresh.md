You are updating repository dependencies in a focused, low-risk way.

Read and follow `AGENTS.md` and any repo-local instructions before making changes.

Task:
- Refresh Go module dependencies in `go.mod` and `go.sum`.
- Refresh base image references in the root `Dockerfile` when there is a clear, low-risk update.
- Keep changes conservative. Prefer patch and minor upgrades. Avoid unnecessary major-version jumps.
- Do not modify `tailwind/Dockerfile`.
- Do not change application code unless a small compatibility fix is required by the dependency update.

Constraints:
- Keep the `go` directive in `go.mod` unchanged unless a dependency update clearly requires a newer Go version.
- If you change the builder image in `Dockerfile`, keep it compatible with the `go` directive in `go.mod`.
- Keep the runtime image minimal.
- Avoid unrelated cleanup or refactors.

Validation:
- Run `go mod tidy`.
- Run `ENABLE_MOCKS=1 go test ./...`.
- Run `docker build -t careme-deps-check .`.

Deliverable:
- Leave the repo ready for a pull request with only the necessary dependency-update changes.
- In the final message, summarize what changed, what you intentionally left alone, and the validation commands you ran.
