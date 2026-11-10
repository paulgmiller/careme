# MN Food Club mock walkthrough

With `ENABLE_MOCKS=1`, open `${PUBLIC_ORIGIN}/mnfoodclub-demo/` on the
Careme test site. The existing `PUBLIC_ORIGIN` must match the test site's public
URL so its forms and handoff links work. These routes are absent when mocks are
disabled. This walkthrough does not change recipe ordering buttons or enable
production MN Food Club ordering.

Choose a known email to demonstrate immediate processing, or a new customer to
demonstrate preserving the list until login. Follow the review link and use the
simulated sign-in button. Hints never sign the browser in automatically. The
request and response are available under the API details on the result page;
the partner token stays server-side. The demo invokes the mock HTTP API handler
in-process, so it needs no network access or partner credentials.

## Standalone mock API

```sh
MNFOODCLUB_MOCK_TOKEN=local-demo-only go run ./cmd/mnfoodclubmock
```

Open `http://localhost:8081/`. To expose it behind an HTTPS test-site proxy, set
`-addr :8081 -origin https://your-test-host.example`. This is a mock service with
fictional data; never give it production credentials or customer information.
The standalone API uses the token you supply, independent of BigCommerce catalog
credentials. The embedded test-site API uses a random internal token and is
intended for the walkthrough; use the standalone command for external API calls.

```sh
curl http://localhost:8081/v1/cart-transfers \
  -H 'Authorization: Bearer local-demo-only' \
  -H 'Idempotency-Key: example-transfer-1' \
  -H 'Content-Type: application/json' \
  -d '{"items":[{"product_id":"101","quantity":3},{"product_id":"202"}],"identity_hints":{"emails":["chef@example.com"]}}'
```

The [proposed OpenAPI contract](mnfoodclub-ordering.openapi.yaml) defines the
request and response. The mock accepts `POST /v1/cart-transfers`, bearer auth,
and an idempotency key. The contract defaults omitted quantities to one and
rejects repeated product IDs with HTTP 400 and `duplicate_product_id`. Results
contain accepted and rejected quantities per product. The mock must follow this
validation before cart changes or pending handoff creation. Matched hints are
`chef@example.com` or `customer-42`; unknown or conflicting hints use
`pending_login`. Storefront brand hints are ignored by this single-brand mock.

| Product ID | Fictional behavior |
| --- | --- |
| `101` | Two packages in stock across the server's demo cart; smallest purchasable size `22 oz` |
| `202` | `product_unavailable` |
| `303` | `no_valid_options` |
| Any other ID | `product_not_found` |

The demo cart begins with one `pantry` package. Each transfer preserves existing
contents; later transfers can exhaust product 101. An all-rejected transfer still
returns `applied`. A new walkthrough creates a fresh key, not a fresh cart.
Restart the server to reset the cart and keys.

Identical requests with the same key return the original response, including
after pending login completes. Changed payloads return `409 idempotency_conflict`.
Concurrent requests share the serialized result. Pending handoffs use random
tokens and apply once through repeated sign-ins. Cart pages show a snapshot
after that transfer, with simulated login required to see results.

All state is shared, fictional, in-memory demo data. Keys have no eviction and
are retained only until restart; this does **not** satisfy production durability
or the 24-hour retry guarantee. The mock does not simulate operational failures,
rate limits, link expiry, real identity matching, or checkout. Localhost uses
HTTP for development; externally visible test links should use HTTPS. Deploy
the production partner integration separately once its contract is agreed.
