# Food Club test stub

The standalone `cmd/mnfoodclubstub` service implements the proposed Food Club
ordering API for `caremetest`. It always returns `applied`, accepts every requested
package, and returns the fixed customer ID `stub-customer`. It does not simulate
login, stock, or product lookup.

The public page at <https://test.careme.cooking/mnfoodclub-stub/> lists all orders,
newest first, including product IDs, variants, quantities, and identity hints.
Use test identities: this page is intentionally public. Reload to see new orders.
Each returned `cart_url` links to that order on this page.

The API requires `Authorization: Bearer caremetest-mnfoodclub-stub`. This is a
non-secret test token, not a real Food Club credential.

- `POST /v1/cart-transfers` takes the documented JSON body and `Idempotency-Key`
  header. Omitted quantities default to one. Repeating the same key and request
  returns the original result; changing the request with the same key returns 409.
- `GET /v1/cart-transfers/{transfer_id}` returns the saved result, or 404.
- Invalid requests, nonpositive quantities, and duplicate product IDs return 400.

Records, normalized requests, responses, and idempotency keys are stored in the
`mnfoodclub_stub_transfers` table in CockroachDB. A unique idempotency key selects
one saved result even for simultaneous requests or requests handled by different
pods. Restarts preserve transfer IDs, cart links, quantities, and identity hints.
The service creates its table at startup and requires `DATABASE_URL`; it never
falls back to memory. There is no reset endpoint.

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

The image pipeline builds `careme-mnfoodclubstub` using the shared Dockerfile.
Deploy a published image with:

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

`deploy/deploy.sh <ref> caremetest` invokes the standalone stub deployment script
and continues to configure Careme's `MNFOODCLUB_ORDER_API_URL` and
`MNFOODCLUB_ORDER_ACCESS_TOKEN`. The production deployment excludes the stub.

## Verify persistence

HTTP unit tests use an explicit test store. To exercise the real database in an
isolated temporary schema, set `MNFOODCLUB_STUB_TEST_DATABASE_URL` and run:

```sh
go test ./internal/mnfoodclubstub -run TestSQLStorePersistence -count=1
```

The test closes and reopens the connection pool, verifies repeat and conflicting
keys, concurrent requests, saved identity hints and quantities, and readiness
failure when the database is unavailable. It drops only its generated test schema.
