# Cart transfers

Careme's shopping list submits a normal browser form to its ordering destination.
The destination handler owns authentication, product matching, cart writes,
idempotency and redirects. The shopping list renders an “Add to cart” form,
a saved cart link, or a failure label,
using a provider lookup during server rendering. It never polls from the browser.

This browser contract differs from the [MN Food Club proposal](mnfoodclub-ordering.openapi.yaml),
which describes a server-to-server JSON API. A future Food Club provider would
receive Careme's form, call that upstream API, and redirect the browser to its
returned cart or account-connection URL. Upstream states stay inside the adapter.

## Browser form

`POST /<destination>/order-transfers` accepts two URL-encoded hidden inputs:

- `transfer_request`: the complete JSON-serialized `orders/types.TransferRequest`,
  containing the displayed `items`, source `location`, and local `return_url`.
- `idempotency_key`: identifies one intended addition for this shopper and
  destination, normally the finalized shopping-list hash.
  The finalized key identifies the items and location; repeated posts reuse the
  saved result without comparing payloads. Redirects use the current validated
  request’s return URL; OAuth callbacks use their saved continuation’s return URL.

`OrderView` supplies the POST URL, serialized request, idempotency key, and button
copy. The template uses a native form with `hx-boost="false"`. There is no HTMX
transfer request, status URL, polling, JSON response, or connect-account control.
A native form cannot set a custom header; an adapter calling Food Club would
forward the form's key in the upstream `Idempotency-Key` header.

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

Kroger sends one package per unique matched UPC. Unmatched items are counted in
the saved result; the shopper reviews amounts and checkout in Kroger's cart.
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
There is no separate provider code or custom JSON decoding.

The shared `TransferRequest` and `Transfer` types describe the submitted request
and stored result inside the handler; they are not a browser JSON API. Transfer IDs use the
idempotency key directly, scoped to the authenticated user and destination.
Records have no automatic expiry; OAuth state expires logically after ten minutes.
See [cache layout](cache-layout.md) for storage details.

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
