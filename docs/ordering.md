# Cart transfers

Careme's shopping list submits a normal browser form to its ordering destination.
The destination handler owns authentication, product matching, cart writes,
idempotency and redirects. The shopping list renders an “Add to cart” form,
a saved cart link, or a failure label,
using a provider lookup during server rendering. It never polls from the browser.

This browser contract differs from the [MN Food Club proposal](mnfoodclub-ordering.openapi.yaml),
which describes a server-to-server JSON API. The Food Club adapter receives
Careme’s form and calls that upstream API. Applied transfers return to the
shopping list; unresolved customers navigate to the returned login handoff.
Upstream states stay inside the adapter.

## Browser form

`POST /<destination>/order-transfers` accepts two URL-encoded hidden inputs:

- `transfer_request`: the complete JSON-serialized `orders/types.TransferRequest`,
  containing the displayed `items`, source `location`, and local `return_url`.
  Each shopping item includes its optional `variant_id` from the saved catalog
  snapshot.
- `idempotency_key`: identifies one intended addition for this shopper and
  destination, normally the finalized shopping-list hash.
  The finalized key identifies the items and location; repeated posts reuse the
  saved result without comparing payloads. Redirects use the current validated
  request’s return URL; OAuth callbacks use their saved continuation’s return URL.

`OrderView` supplies the POST URL, serialized request, idempotency key, and button
copy. The template uses a native form with `hx-boost="false"`. There is no HTMX
transfer request, status URL, polling, JSON response, or connect-account control.
A native form cannot set a custom header. The Food Club adapter derives its
upstream `Idempotency-Key` from the authenticated Careme user and form key, since
Food Club scopes keys across the whole integration rather than by shopper.

The authenticated shopper comes from the verified Careme session, never from
submitted JSON. A shopper whose session has expired is redirected to sign in,
then back to the shopping list to submit again. `return_url` must be an
application-local path; it is used for sign-in return and for returning to the
shopping list after a transfer.

## Kroger navigation

1. The POST checks the shopper’s encrypted per-user token and refreshes it when needed.
2. If a Kroger connection is needed, it immediately returns a 303 redirect to
   Kroger OAuth. The shopper does not have to click a separate connect button.
3. OAuth continuation data preserves the request separately from order transfers.
   `/kroger/callback` validates the state cookie, ownership, and ten-minute age. It exchanges the authorization code, saves the
   encrypted token, and resumes the saved request.
4. A saved outcome returns a 303 redirect to the saved local shopping list.
   Expired access tokens are refreshed internally.

No products are added on shopping-list page load. There are no transfer-status
or separate connection GET routes. Reloading the shopping list reads the saved
handoff to choose the form, cart link, or failure label. Reposting consults the
saved transfer without resubmitting products.

Kroger sends the requested `buy` quantity for each unique matched UPC, rounding
up to whole packages and defaulting to one when `buy` is absent. Successful writes record
accepted or rejected package quantities for each displayed item; the shopper reviews amounts and checkout in Kroger's cart.
A cart transfer never checks out or places an order.

## Failures and duplicate protection

OAuth denial, a missing code, or token exchange/storage errors return ordinary
HTTP errors on the provider route. They do not create an order transfer.

After authentication succeeds, the handler creates a pending transfer. Product
lookup errors, no matched products, and cart write errors are saved as failed.
Both successful and failed transfers redirect to the request’s shopping-list URL.
The shopping list replaces the form with “Add to cart failed.” for a failed
transfer. There is no retry control, and repeated posts or OAuth callbacks reuse
the saved result without another cart submission.

Pending and applied records show the cart link. A failed final save returns an
HTTP error and leaves the record pending. A cart write error can occur after
Kroger accepts products; failed therefore means success was not confirmed.

Only the shared `Transfer` result is stored in the transfer record. The request
is used during submission; OAuth continuation data preserves it separately
while connecting an account. Shopper
tokens are encrypted and keyed by Careme user, not transfer. Their expiry comes
from Kroger’s `expires_in`; the handler refreshes them one minute before expiry.

TODO: use cache ETags and `IfMatch` for transitions. Updates are unconditional
for now: concurrent attempts can overwrite results and can duplicate additions.
A failed final save leaves the record pending. This is not a guarantee of
at-most-once cart writes across concurrent requests.

The typed `TransferStatus` values are `pending`, `failed`, and `applied`.
Pending covers an in-flight attempt or a saved partner handoff awaiting login.
There is no separate provider code or custom JSON decoding.

The shared `TransferRequest` and `Transfer` types describe the submitted request
and stored result inside the handler; they are not a browser JSON API. Kroger
and mock transfer IDs use the idempotency key directly; Food Club retains its
partner-issued transfer ID. Local cache keys are scoped to the Careme user and
form idempotency key.
Records have no automatic expiry; OAuth state expires logically after ten minutes.
See [cache layout](cache-layout.md) for storage details.

## MN Food Club partner API

Set both `MNFOODCLUB_ORDER_API_URL` and `MNFOODCLUB_ORDER_ACCESS_TOKEN` to enable
`POST /mnfoodclub/order-transfers` for MN Food Club, TC Farm, and Nelson Grass
Farm delivery locations. They are separate from BigCommerce catalog credentials.
The API remains a proposal; no production host or token is assumed. HTTPS is
required except for HTTP localhost endpoints used in tests.

The adapter sends JSON to `/v1/cart-transfers` with the partner bearer token and
an integration-wide idempotency key derived from the Careme user ID and list key.
Only the authenticated shopper’s account email is sent as an identity hint.
Careme IDs are never passed as Food Club customer IDs. Customer-ID, ZIP, and
storefront-brand hints are currently omitted.

Each product is sent once. Package counts come from `buy`, rounded up to whole
packages; a missing count defaults to one. Duplicate product quantities are
combined. Recipe measurements such as “2 cups” are never converted to package
counts. Items without a product ID are recorded as local rejections and not sent. If no
products can be sent, save a failed transfer without calling the partner.
The selected catalog variant is included when available in the finalized
shopping snapshot. Older snapshots and items without a selected variant omit it,
using the partner’s documented default variant selection.

An `applied` response saves the shared transfer result and returns the browser
to the shopping list. The shared result contains each product’s accepted and
rejected package quantities and rejection reason, with local rejections appended.
Partial and all-rejected responses remain applied and retain the supplied cart
link. The partner’s product IDs, totals, statuses, and cart URLs must be valid;
incomplete results fail the transfer instead of silently omitting products.

The partner’s `pending_login` response is converted to the shared `pending`
status inside the Food Club adapter and immediately redirects the browser to
the partner’s opaque handoff URL. Loading the shopping list later performs one
server-side GET for the saved partner transfer ID.
Once applied, the result and cart URL are updated and no further GET is needed.
While login remains pending, Review cart opens the saved partner handoff URL.
Lookup errors, including HTTP 404, fail the page; they never create a new transfer.
There is no browser polling or callback route.

Transfers are reserved before the POST and never automatically retried.
Operational errors or invalid responses are saved as failed, and the browser
returns to the shopping list’s failure label. Item results record only confirmed
acceptance or rejection; an API failure does not invent product rejections. A failed save returns HTTP 500.
Repeated native posts reuse the saved outcome; pending transfers redirect to
the handoff again. No request wrapper or customer-ID mapping is persisted. The partner response
embeds the shared `Transfer` plus `user_id`; TODO: save that customer-ID mapping
for future identity hints.

## Local development and E2E tests

With `ENABLE_MOCKS=1`, the mock order provider handles every location ID instead
of registering real ordering providers. No Kroger credentials, cart-token key,
or OAuth callback registration are needed. Generate and finalize a shopping
list, then submit “Add to mock cart”. The provider accepts all displayed items,
saves a shared `Transfer`, and redirects back to the shopping list. “Review cart”
opens the local `/mock/order-transfers/<id>` endpoint, which displays the saved
result as JSON. Repeated submissions reuse the result without adding again.

Mock transfers use `mock_order_transfers/<user ID>/<idempotency key>` and remain
separate from real provider records. The normal web E2E test follows this native
form POST through the redirect and checks the resulting shopping list and cart.
Kroger's own tests cover OAuth and upstream API contracts separately.

## Adding a destination

Register its POST and authentication callback routes through
`providerregistry.Factory.RegisterOrders`, then supply its location match and
cache-bound `OrderView` builder through `Factory.NewOrderViewBuilder(cache)`.
The adapter translates the shared request to the upstream
API and owns redirects and failure handling. Provider state does not belong in
the recipe handler or shared shopping-list UI.

The [MN Food Club mock walkthrough](mnfoodclub-mock.md) remains available at
`/mnfoodclub-demo/` with mocks enabled. It demonstrates the proposed upstream
partner API, not a production recipe ordering destination.

## Saved cart link on the shopping list

The server asks the configured provider for this signed-in viewer's saved
transfer using the finalized list key. Kroger reads one saved transfer record.
Completed and pending writes replace the form with “Review cart (opens a new tab)”. Failed transfers
show “Add to cart failed.” without a form. Missing transfers keep Add to cart.
Read or decoding errors fail the page request. There is no browser status fetch.
The saved handoff remains after checkout or cart edits; this is transfer history,
not a live cart read. A different finalized list has its own idempotency key.
