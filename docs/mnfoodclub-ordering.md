# MN Food Club cart handoff proposal

This is a proposed contract for partner review, not a deployed partner endpoint. The
[OpenAPI document](mnfoodclub-ordering.openapi.yaml) is the machine-readable
handoff for MN Food Club and the basis for a later Careme client. A separate
[mock API and test-site walkthrough](mnfoodclub-mock.md) demonstrates the flow
with fictional data; production Careme ordering is not enabled.

## Request and customer matching

Careme's server sends `POST /v1/cart-transfers` with a dedicated partner-issued
bearer token and an `Idempotency-Key`. No customer OAuth connection is required.
The proposed server hostname uses `.example` and must be replaced with the
partner's actual endpoint before use.

```json
{
  "items": [
    {"product_id": "101", "variant_id": "1005", "quantity": 3},
    {"product_id": "202"}
  ],
  "identity_hints": {
    "emails": ["chef@example.com", "kitchen@example.com"],
    "user_id": "customer-42",
    "storefront_brand": "preferred-storefront",
    "zip_code": "55401"
  }
}
```

Product IDs are opaque strings from the catalog, currently parent BigCommerce
product IDs on Careme's side. Quantities count purchasable packages, default to
one, and must be specified on a single item per product ID. To request two
packages, send `{"product_id":"101","quantity":2}`. Repeated product IDs return
HTTP 400 with `duplicate_product_id`, even when quantities or variant IDs differ,
before any cart changes or pending handoff creation. Recipe measurements such
as “2 cups” are not package counts and must not be sent as quantities.

An optional `variant_id` identifies the exact variant of `product_id` to add.
Use opaque strings for both IDs. If that variant does not belong to the product
or cannot be purchased, reject the item's quantity without substituting another
variant. Each transfer may request only one variant per product, consistent
with the duplicate-product rule.

All identity hints are optional. Emails can identify a customer without a prior
connection. MN Food Club owns matching, including conflicts between a saved ID
and emails, ambiguous matches, and unknown hints. An unresolved customer gets a
login handoff rather than an error. The storefront brand is a preferred site for
branding and the destination URL, not a product brand or an account boundary.
MN Food Club defines the recognized brand values.

The optional `zip_code` is another customer-matching hint, supplied as a string
to preserve leading zeros. It accepts a five-digit ZIP or ZIP+4. MN Food Club
owns how it combines the ZIP code with the other hints.

## Matched and unmatched flows

For a matched customer, immediately add accepted quantities to their existing
cart, preserving its other contents. Return `status: applied`, their reusable
`user_id`, a cart URL, and exactly one result per requested product ID.
The ID can be retained by Careme and passed as a hint on future requests; this
proposal does not add that persistence yet.

Each result reports accepted and rejected quantities. Accepted means
added by this transfer, not the cart's total quantity. Add partial quantities when
possible: a request for three packages can report two accepted and one rejected
with `insufficient_stock`. Accepted plus rejected must equal the request quantity
(defaulting to one); the response does not repeat that quantity. A positive
rejected quantity requires a reason; callers must tolerate new reason codes.

**Use the supplied variant; when no variant is supplied, choose the smallest
purchasable size for now.** Quantities count packages of the selected variant or
size. MN Food Club chooses valid defaults for other required choices, such as
flavor. The response does not include variant IDs or selected options. If no valid selection is possible, reject that product
and continue processing the others. Unknown or unavailable products are also individual rejections.
Even if all products are rejected, return HTTP 200, `applied`, the product results,
and a usable cart URL. “Applied” describes completed processing, not full product
acceptance. Existing checkout and order-cutoff rules remain MN Food Club's.

For an unmatched customer, return HTTP 200, `status: pending_login`, a transfer
ID, and a handoff URL. Preserve the complete product list, including supplied
variant IDs, without assessing acceptance yet. The shopper can review it on
MN Food Club, log in, and have the transfer added to their cart. Preserve the request through login
and apply it at most once. MN Food Club shows the eventual results; Careme can
retrieve the resolved user ID and product results through the status endpoint
below. No webhook or result callback is required.

Cart links use HTTPS and MN Food Club-controlled storefronts. Identity hints do
not sign the shopper into a browser or bypass normal access checks. Pending
links use opaque tokens rather than emails or customer IDs in their URLs.
MN Food Club owns link lifetime and the expired-link experience, while ensuring
replayed successful responses remain usable during the retry window. Use
`Cache-Control: no-store` for responses containing identifiers and handoff links.

## Retrieve transfer status after login

Careme can call `GET /v1/cart-transfers/{transfer_id}` with its partner bearer
token to retrieve the current state. The ID comes from the original POST; the
GET requires no idempotency key and never changes a cart. Only the integration
that created the transfer can read it.

Before the shopper completes login and cart application, GET returns
`pending_login`, the transfer ID, and the handoff URL. Once applied, it returns
`applied`, the same transfer ID, the resolved `user_id`, cart URL, and each
product's accepted/rejected quantities. Careme can then retain the user ID for
future requests. Results describe that transfer, including all-rejected outcomes;
later edits to the customer's cart do not change its product results.

The OpenAPI document includes before-login and after-login examples for the
same transfer. GET reflects the latest state; replaying the original POST with
its idempotency key still returns the original POST response, even if that
response was `pending_login` and the shopper has since completed the handoff.

MN Food Club must retain status while the pending handoff link is usable and
for at least 24 hours after application completes, with a minimum of 24 hours
from creation. Longer retention is partner-defined. Poll with backoff while
pending and honor `Retry-After` on HTTP 429. Stop polling at `applied` or HTTP 404.
Unknown, expired, and other integrations' transfers all return HTTP 404 without
customer information. A missing status does not mean no products were added;
do not create a new transfer automatically. Retry failed GETs with backoff.

## Retries and failures

The proposal requires idempotency protection for at least 24 hours from first
receipt, scoped to the partner integration. Reserve the key before modifying a
cart or creating a pending handoff. Repeating the same key and payload returns
the original successful response without another addition or handoff. Keep the
original key even after an operational error; partial cart writes must not be
repeated. Applying a pending transfer remains at-most-once even after the API's
retry window ends.

- A deliberate new addition uses a new key.
- A changed payload with the same key returns HTTP 409, `idempotency_conflict`.
- A concurrent request either shares the result or returns HTTP 409,
  `transfer_in_progress`, with `Retry-After`.
- HTTP 429 uses `Retry-After`; retry the unchanged payload with the same key.
- Timeouts and HTTP 500/503 can leave the outcome uncertain. Retry with the same
  key within the retention window. After that window, verify the outcome with
  MN Food Club before attempting another addition.
- Malformed requests, duplicate product IDs, and invalid quantities return
  HTTP 400. Authentication and authorization failures return HTTP 401/403.
  These do not modify a cart.

Missing customers and rejected products are business outcomes, not operational
failures. A service outage must not be reported as an ordinary product rejection
or a successful handoff unless the request was actually preserved or processed.

## References and alternative handoff

The proposal reuses Kroger's product-and-package-quantity model and preserves
existing cart contents. It uses the Instacart branch's approach of returning a
destination-owned link when the shopper must continue elsewhere. It adds
per-product quantity results and identity hints for MN Food Club's needs. The
BigCommerce branch and current MN Food Club provider supply catalog context;
this API does not expose BigCommerce's cart implementation or require a new
shared ordering abstraction. See [Careme ordering handoffs](ordering.md).

A simpler alternative is to send the shopper directly to an MN Food Club URL
containing product IDs, quantities, and supported hints. MN Food Club would own
matching, product assessment, login, and cart application in the browser. Careme
would receive no structured acceptance results. This alternative is for
discussion; its route, encoding, length limits, and duplicate protection are not
specified in v1. Avoid putting emails or persistent customer IDs directly into
query strings; an opaque handoff token or browser POST could carry those hints
if needed. No integration bearer token belongs in a browser URL.

MN Food Club should confirm the production URL and credentials, storefront brand
values, matching rules, the smallest-size policy and defaults for other options,
link lifetime, rate and payload limits, and the proposed minimum 24-hour
idempotency and transfer-status retention. These are partner
deployment details and policy confirmations, not claims about an existing API.
