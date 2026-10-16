# Ordering adapters

Ordering is the optional third step of a store integration: add locations, add
staples, then add ordering when supported. See [Adding a provider](../README.md#adding-a-provider)
for those interfaces and their registration points. Ordering destinations may
also accept existing lists without providing a location backend or staples.

`internal/ordering` coordinates handoffs to grocery providers. Recipes load a finalized purchase snapshot, including current wine selections, and pass a provider-neutral request. Providers are constructed in application setup and injected into the recipe handler. Only Kroger is implemented.

A request carries source-store identity separately from the ordering destination. Item product IDs are qualified by provider; a receiving adapter must not interpret another catalog's IDs as its own. Names and required-amount text remain available for matching. Structured measurements, UPCs, package sizes, and buy counts are optional. Buy counts refer to source products and are not automatically destination package counts.

Adapters implement `Supports`, `Presentation`, `Prepare`, and `Submit`, plus stable `ID` and `Workflow` methods. `Prepare` validates or resolves items without changing an external cart or creating a shopping page. Its opaque `Data` belongs to the adapter and is never persisted. `Submit` performs the external operation. A cart adapter reports the actual package quantities it submitted and any known gaps; lookup failures abort preparation. A shopping-link adapter can pass names through without resolving products, and reports page creation rather than cart additions.

Account connection is optional. An adapter can return `ConnectionRequired`; adapters needing browser authorization also implement `StartAuthorization(http.ResponseWriter, *http.Request, ordering.Request)` and register their callback in application setup. A verified callback resumes the current finalized snapshot through `ResumeOrdering`. Credentials and provider configuration live on the adapter.

Cart transfers reserve provider/user/list identity before submission. An uncertain external response or a failure to persist confirmation must not trigger an automatic second cart submission. Shopping links instead reuse successful results by request-content fingerprint and expiry; creation failures are retryable. See [cache layout](cache-layout.md).

## Future Instacart shopping-link adapter

The [Create shopping list page endpoint](https://docs.instacart.com/developer_platform_api/api/products/create_shopping_list_page) creates a URL using an application API key. Users choose a store and add products after opening it. A future adapter would use `ShoppingListLink`, map title, line-item names, optional `line_item_measurements`, and the link-back URL, then return `ShoppingListCreated` with `products_link_url`. It does not need shopper OAuth or pre-resolved catalog matches.

Instacart-specific unit validation, identifier selection, filters, and request encoding belong to that future adapter. Product IDs and UPCs must not be sent together. Unsupported structured measurements should fail preparation contextually; the shared module does not guess units from free text. No Instacart integration or credentials are included in this refactor.
