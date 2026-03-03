# Refactor Options for `internal/recipes/server.go`

## Context
`internal/recipes/server.go` had grown to ~900 lines and mixed multiple concerns:
- route registration
- read/render handlers
- mutation handlers
- generation lifecycle and async work
- small utility helpers

This makes it harder to navigate and increases change risk when touching one endpoint.

## Option 1: Split By Concern (Recommended)
Create multiple files in the same `recipes` package and keep behavior identical:
- `server.go`: core types, constructor, route registration, shared constants/helpers
- `server_read.go`: read paths (`/recipes`, `/recipe/{hash}`, ingredients, not-found flow)
- `server_actions.go`: mutation endpoints (`question`, `feedback`, `save`, `dismiss`, `regenerate`, `finalize`)
- `server_async.go`: async generation, spinner page, wait

Pros:
- lowest risk (no architecture rewrite)
- immediately improves readability and ownership boundaries
- keeps tests and public behavior stable

Cons:
- does not deeply reduce handler complexity
- core orchestration still lives in HTTP layer

Effort: small/medium
Risk: low

## Option 2: Introduce Service Layer + Thin Handlers
Move business logic into service objects (`RecipeReadService`, `RecipeActionService`) and keep handlers as request/response adapters.

Pros:
- clearer test seams
- business logic decoupled from transport

Cons:
- wider signature churn
- higher short-term migration effort

Effort: medium/high
Risk: medium

## Option 3: Split Into Sub-packages (`internal/recipes/server/*`)
Create dedicated sub-packages for handlers, auth/session guards, and render adapters.

Pros:
- strongest modular boundaries
- easier long-term scaling of endpoint count

Cons:
- package/API churn
- more imports and interface surface changes
- larger immediate refactor blast radius

Effort: high
Risk: medium/high

## Decision
Implement **Option 1** now. It provides the best immediate maintainability gain with minimal risk and no behavior changes.

## Follow-up: Real Package Boundary
Because Go encapsulates at package level (not file level), this follow-up extracts selection state into an independent package:
- `internal/recipes/selectionstate`

Moved concerns:
- selection state model (`saved`/`dismissed`)
- cache persistence for selection state
- hash-to-recipe resolution helper

Server impact:
- `internal/recipes` now delegates selection persistence/behavior to `selectionstate.Store`
- server remains focused on HTTP orchestration

## Follow-up: Generation Package
Async generation orchestration is extracted to:
- `internal/recipes/generation`

Moved concerns:
- background task lifecycle (`Kick`/`Wait`)
- recent recipe filtering window (last 14 days)
- standardized generate/save error handling for async runs

Server impact:
- `internal/recipes` now delegates generation goroutine management to `generation.Runner`
- handlers keep orchestration intent but no longer own wait-group mechanics
