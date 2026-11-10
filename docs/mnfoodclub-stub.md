# Food Club test stub

The standalone `cmd/mnfoodclubstub` service implements the proposed Food Club
ordering API for `caremetest`. Transfers without a stub login cookie return
`pending_login`. The browser handoff asks for a test name, stores it in a cookie,
and applies the transfer. Applied transfers accept every requested package and
return that name as `user_id`. Stock and product lookup are not simulated.

The public page at <https://test.careme.cooking/mnfoodclub-stub/> lists all orders,
newest first, including product IDs, variants, quantities, and identity hints.
Use test identities: this page is intentionally public. Reload to see new orders.
For pending transfers, `cart_url` opens `/handoff/<transfer_id>`; applied cart
URLs link to the order on this page.

The API requires `Authorization: Bearer caremetest-mnfoodclub-stub`. This is a
non-secret test token, not a real Food Club credential.

- `POST /v1/cart-transfers` takes the documented JSON body and `Idempotency-Key`
  header. Omitted quantities default to one. Repeating the same key and request
  returns the original POST result, even after login; changing the request with
  the same key returns 409.
- `GET /v1/cart-transfers/{transfer_id}` returns the current saved status and result, or 404. This endpoint never applies a transfer.
- Invalid requests, nonpositive quantities, and duplicate product IDs return 400.

Records, normalized requests, responses, and idempotency keys are stored in the
`mnfoodclub_stub_transfers` table in CockroachDB. A unique idempotency key selects
one saved result even for simultaneous requests or requests handled by different
pods. Restarts preserve transfer IDs, cart links, quantities, and identity hints.
The service creates its table at startup and requires `DATABASE_URL`; it never
falls back to memory. There is no reset endpoint.

## Stub login

Careme calls the API server-to-server, so those requests do not contain the
shopper's browser cookies. The API returns a pending handoff without accepted
item results or a `user_id`. Identity hints alone never log the shopper in.

Opening the handoff asks for any test name. Submitting it sets the
`mnfoodclub_stub_name` cookie and atomically changes that transfer to `applied`.
An existing cookie applies a subsequent pending handoff immediately. The name
is base64url-encoded in the cookie so Unicode names work; the cookie is scoped
to the stub path, HttpOnly, SameSite=Lax, and Secure on HTTPS. This is a test
identity, not real authentication. Use test names only.

The applied result is saved in CockroachDB, so status GETs from Careme see it
without browser cookies. Reopening or resubmitting a handoff does not add items
again or change the original applied user. Reload the Careme shopping list to
refresh a pending result into its applied cart link. Replaying the original
idempotent POST still returns its initial response, as required by the contract.
Clear this stub cookie in your browser to exercise the name form again.

Existing applied transfers remain applied after this update. Startup adds a
nullable `post_response` column without interrupting the previous binary; old
applied records use their existing response as their original POST response.

## Run locally

```sh
export DATABASE_URL='postgresql://user:password@localhost:26257/mnfoodclub_stub?sslmode=require'
go run ./cmd/mnfoodclubstub -addr :8080 -public-url http://localhost:8080
```

```sh
curl http://localhost:8080/v1/cart-transfers \
  -H 'Authorization: Bearer caremetest-mnfoodclub-stub' \
  -H 'Idempotency-Key: local-example' \
  -H 'Content-Type: application/json' \
  -d '{"items":[{"product_id":"101","variant_id":"1005","quantity":3}]}'
```

Open <http://localhost:8080/> to see the order. `-public-url` may include a path
prefix; all service routes, including `/ready`, are mounted beneath it.

## Deployment

Build and publish `docker.io/paulgmiller/mnfoodclubstub:<commit>` using the shared
Dockerfile with `CMD_PATH=./cmd/mnfoodclubstub`. Deploy that published commit
explicitly with:

```sh
bash deploy/mnfoodclub-stub.sh <ref>
```

The script creates namespace `mnfoodclub-stub` first. Cluster provisioning creates
that namespace's CockroachDB database and `roachkey` secret. The Deployment
imports `DATABASE_URL` from `roachkey`; no credentials are copied from caremetest.
The script waits for the secret and the new Deployment to be ready before
removing the legacy stub Deployment, Service, and Ingress from `caremetest`.
The namespace and workload manifests are `deploy/mnfoodclub-stub-namespace.yaml`
and `deploy/mnfoodclub-stub.yaml`.

The public URL stays `https://test.careme.cooking/mnfoodclub-stub/`. Its Ingress
and Service live in the new namespace. `GET /ready` verifies database access;
`GET /live` checks the HTTP process without depending on database availability.
The database-backed store starts empty on this move; old memory-only orders are
not imported. Careme links to those old IDs consequently return 404.

Careme's deploy script never applies the stub manifests or rolls out the stub.
Caremetest continues to configure `MNFOODCLUB_ORDER_API_URL` and
`MNFOODCLUB_ORDER_ACCESS_TOKEN` to connect to the independently deployed service.
Run the standalone stub script explicitly whenever the stub should be upgraded.

## Verify persistence

HTTP unit tests use an explicit test store. To exercise the real database in an
isolated temporary schema, set `MNFOODCLUB_STUB_TEST_DATABASE_URL` and run:

```sh
go test ./internal/mnfoodclubstub -run TestSQLStorePersistence -count=1
```

The SQL tests also cover pending login across connection-pool restarts,
simultaneous handoff submissions, and migration of older applied records. Run
all of them with `-run TestSQL`. HTTP tests exercise the real Careme adapter's
redirect, browser cookie login, and cookie-free status refresh. The persistence
test verifies repeat and conflicting keys, concurrent requests, saved identity
hints and quantities, and readiness failure when the database is unavailable. It drops only its generated test schema.
