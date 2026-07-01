# Generic Conversion And OTel Events

## Summary

Add one canonical conversion/event layer used by Google Ads today and by configured Reddit/OpenAI-style ad snippets later. Track these successful events: `sign_in`, `recipe_generation`, `recipe_regeneration`, `location_lookup`, `recipe_save`, `recipe_question`, and `recipe_cooked`.

## Key Changes

- Add `internal/conversions` with canonical event constants, OTel counter recording, cache-backed one-time guards, and HTMX trigger helpers.
- Wire OTel metrics export in `internal/logsetup` alongside existing traces/logs when `OTEL_EXPORTER_OTLP_ENDPOINT` is configured.
- Keep `GOOGLE_TAG_ID` and legacy `GOOGLE_CONVERSION_LABEL` for sign-in, and add event-label mapping with `GOOGLE_CONVERSION_LABELS_JSON`.
- Add trusted generic browser snippets with `CONVERSION_HEAD_SNIPPET` and `CONVERSION_EVENT_SCRIPT` so Reddit/OpenAI-style pixels can be configured without platform-specific Go code.

## Event Semantics

- `sign_in`: new local user only.
- `recipe_generation`: after initial background generation successfully saves the shopping list.
- `recipe_regeneration`: after regenerate successfully saves the shopping list.
- `location_lookup`: after `/locations?zip=...` renders successfully.
- `recipe_save`: after recipe selection and user profile save both succeed.
- `recipe_question`: after answer generation and thread save both succeed.
- `recipe_cooked`: after feedback save succeeds and submitted `cooked=true`.

## Tests

- Unit-test conversion recording, one-time guards, Google label mapping, and browser script rendering.
- Update handler/template tests for sign-in, generation/regeneration, location lookup, save, question, and cooked events.
- Run `go test ./...`, `golangci-lint run ./...`, and `tailwind/generate.sh` if template output changes.

## Assumptions

- Ad snippets are trusted deployment config, not user input.
- High-cardinality values like recipe hash, user ID, and ZIP are not OTel metric attributes.
- “ChatGPT ads” is handled as a generic browser pixel/snippet sink rather than a server API.
