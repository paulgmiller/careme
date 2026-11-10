# MN Food Club transfer API: meeting decisions and follow-ups

This is the working handoff from the November 3, 2026 meeting and Paul's
subsequent clarifications. It records decisions and pending implementation;
it is not a claim that the partner API is deployed or that these changes ship.

## Sources

- [Meeting recording](https://fathom.video/share/2Ai5C9Wx-iFfzb9xqRzBA-QPQg2yxtHx).
  Paul supplied the transcript in the conversation.
- [Partner artifact](https://claude.ai/artifact/PUe29sfxEZSqZtpCVcwijB?sk=jHlBKxpY4eaxU0bhFJvOnw).
  Its contents could not be retrieved: the document request returned HTTP 403.
  The excerpts in the conversation do not establish its complete JSON shape.
- [Existing Careme proposal](mnfoodclub-ordering.md) and
  [OpenAPI schema](mnfoodclub-ordering.openapi.yaml). These describe the older
  contract and need reconciliation with the partner artifact.

## Agreed direction

- Careme combines ingredients across selected recipes into one shopping list,
  accounting for package sizes, before transferring purchase quantities.
- Use "Add to shopping list" for recipe selection and "Build shopping list"
  for finalization. Use "delivery" for the partner-facing action and review
  link. Adding products does not itself mean an order or delivery is confirmed.
- Preserve store context for visitors referred through MN Food Club, TC Farm,
  or Nelson Grass Farm landing pages, including navigation back to that store.
  Direct Careme visitors retain normal store discovery and selection.
- Do not add ZIP collection for store referrals now. Jack explained that the
  selected store is sufficient to start; ZIP handling is deferred.
- Wait for the partner catalog API before implementing store-specific catalog
  filtering. The partner will exclude memberships, produce shares, gift cards,
  digital products, and alcohol from that API. AI ingredient grading is not a
  substitute for those eligibility rules.
- Send the signed-in Careme user's ID as `careme_user_id`, and the selected
  store identity, using the partner artifact's exact field names and nesting.
  Never source the Careme user ID from browser-submitted JSON.
- Send `link_id` when available. Persist the partner-issued link for subsequent
  transfers. The old proposal's `user_id` means a partner customer ID and must
  not be repurposed as the Careme/Clerk ID.
- Launch with linked/signed-in partner shoppers first; guest support can follow.
  Refresh pending transfer status server-side when the shopper returns. A
  webhook is unnecessary initially.
- Paul confirmed that email sign-ups require email verification. Email remains
  an identity hint. This confirmation came from Paul; repository email lookup
  alone does not establish the production signup configuration.
- Paul accepts his own name being visible on the test-orders page. The stub is
  for the test environment and is intended to be retired when the partner API
  is available. Do not expand this into authorization to expose other users.
- Enable exponential-backoff retries and respect `Retry-After` once the partner
  honors idempotency keys, preserving the same key and payload across retries.

## Current implementation facts

- `internal/providers/mnfoodclub/order.go` authenticates the request before
  sending a transfer and obtains the email through the auth client.
- The outgoing idempotency key is SHA-256 of the authenticated user ID, a NUL
  separator, and the shopping-list key. The list hash incorporates the selected
  store and recipe parameters but does not itself include the user ID.
- Location IDs are `mnfoodclub_delivery`, `tcfarm_delivery`, and
  `nelsongrassfarm_delivery`. The intended partner identifiers discussed are
  `mnfoodclub`, `tcfarm`, and `nelsongrassfarm`; confirm against the artifact.
- Earlier uncommitted work adds `identity_hints.storefront_brand`, using display
  names derived from location IDs, and tests all three stores. This is interim
  work: reconcile it with the partner artifact rather than treating it as the
  final schema. The existing local docs still describe `storefront_brand`.
- The existing provider shares a catalog across stores, adds static produce
  shares, and fetches wines. Those behaviors await the partner catalog API.
- Transfer responses currently expose a partner `user_id`; the adapter has
  TODOs for persisting the customer mapping. `link_id` is not implemented yet.
- The client currently appends `/v1/cart-transfers` to its configured base URL.
  `/partners/careme/v1/cart-transfers` was discussed as the partner route;
  reconcile base-URL configuration with the final contract.

## Immediate implementation task

1. Obtain the partner artifact's request schema, especially the placement of
   `careme_user_id` and `store`, and the exact store identifier values.
2. Set `careme_user_id` from `GetUserIDFromRequest`, after authentication.
3. Map the validated selected location ID to the partner store identifier;
   do not trust submitted location display names or submitted identity hints.
4. Replace the interim brand field with the agreed schema. Preserve email,
   ZIP, product IDs, variant IDs, quantities, and idempotency behavior.
5. Keep the local stub and contract documentation consistent with the new
   request shape. Regenerate published HTML documentation if its source changes.
6. Test the serialized API request for all three stores and verify that a
   browser cannot spoof the authenticated Careme user ID or store display name.
7. Run `./task.sh verify-go`, inspect the final diff, and run `git diff --check`.

The previous interim brand changes passed `./task.sh verify-go`. A separate
targeted test invocation failed because sandbox restrictions blocked local
HTTP sockets; its escalation was interrupted. That was an environment failure.
Run verification again after implementing the final identity fields.

## Subsequent work

- Persist and reuse `link_id` once its response shape, scope, and lifecycle are
  known. Cover creation and pending-login status resolution.
- Update recipe selection and delivery wording, then implement persistent
  store-context navigation through sign-in and return visits.
- Coordinate weekly email timing using store order cutoffs and timezones.
  A favorite store can supply the store preference; scheduling still needs
  the actual cutoff rules.
- Display an appropriate unavailable state for `store_disabled`; Paul suggested
  "Delivery not available."
- Implement catalog filtering and explicit exclusions after receiving the API.

## Open contract details

- Exact request nesting and canonical store values in the partner artifact.
- Where `link_id` is returned, whether it spans stores, and how expiry or
  revocation is communicated.
- Whether item labels are required and what they are used for.
- The distinction between `list_id`, partner `transfer_id`, and idempotency key.
- Variant/package semantics, partial acceptance, and per-item rejection output.
- Whether transfers target an existing delivery, the next eligible delivery,
  or a shopper-selected delivery window.
- Link/status retention and idempotency retention, retryable responses, and
  how deliberate second additions receive a new key.
- Partner implementation schedule and a test endpoint restricted to Paul's
  account if that enables earlier integration testing.

## Working boundary

The active coding request is identity fields and selected-store identity.
The remaining follow-ups are recorded for later work, not authorization to
implement every item in this document in the same change. Do not send messages
to the partner. Do not include credentials or personal incident-contact details
in repository documentation.
