# HTMX Migration Plan

## Goal
Move interactive UI behavior toward server-rendered HTML with [HTMX](https://htmx.org/docs/) and away from custom page-level JavaScript or SPA patterns.

## Frontend Direction
- Prefer HTML-first server rendering.
- Prefer HTMX for partial updates and form interactions.
- Keep JavaScript only when browser APIs are required (for example clipboard access, auth SDKs, or third-party embeds).
- Do not introduce SPA frameworks for routine UI interactions.

## Page-by-Page Plan

### `internal/templates/home.html`
- Current state: no custom interaction JS.
- Plan: optional future HTMX ZIP lookup preview, otherwise keep as-is.
- Priority: low.

### `internal/templates/locations.html`
- Current state: JavaScript-heavy interactions.
- Plan:
  - Use `hx-post` for favorite store updates.
  - Swap updated server-rendered list fragment in place.
  - Use semantic `<details>` for instruction panel toggle.
- Priority: high.

### `internal/templates/shoppinglist.html`
- Current state: most JavaScript-heavy page.
- Plan:
  - Phase 1: replace simple toggles with semantic HTML where possible.
  - Phase 2: convert save/dismiss/finalize interactions to HTMX endpoints with fragment swaps.
  - Keep minimal JS for clipboard copy (`navigator.clipboard`).
- Priority: high.

### `internal/templates/recipe.html`
- Current state: form uses full POST+redirect flow.
- Plan:
  - Convert question submission to HTMX.
  - Return and swap/append question thread fragments from server.
- Priority: medium.

### `internal/templates/user.html`
- Current state: full page POST roundtrips.
- Plan:
  - Convert preference and recipe-add forms to HTMX partial updates with inline success/error messaging.
- Priority: medium.

### `internal/templates/spinner.html`
- Current state: meta refresh polling.
- Plan:
  - Replace meta refresh with HTMX polling against recipe status endpoint.
- Priority: medium.

### `internal/templates/auth_establish.html` and `internal/templates/clerk_refresh.html`
- Current state: required Clerk auth JavaScript.
- Plan: keep JavaScript (HTMX is not a replacement for Clerk browser SDK behavior).
- Priority: no migration planned.

### `internal/templates/mail.html`
- Current state: static email template.
- Plan: no HTMX relevance.
- Priority: none.

## Suggested Rollout
1. Complete `locations` migration as the reference pattern.
2. Tackle `shoppinglist` in small, testable phases.
3. Move `recipe` and `user` forms to HTMX partials.
4. Revisit spinner polling once status endpoints are stable.

## Implemented Example
`locations` has already been prototyped with HTMX:
- Added `POST /locations/favorite` to support HTMX and non-HTMX fallback.
- Replaced client-side favorite update logic with server-rendered swap.
- Removed toggle script by switching instruction panel behavior to `<details>`.
- Added endpoint tests for HTMX response and non-HTMX redirect paths.
