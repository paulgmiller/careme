# mnfood.club partner demo

Fixed inventory transcribed from the two user-supplied photos of **TC Farm Produce
Weekly Feature**, headed **September 21st–26th deliveries**. The original request
said September 21–24; the supplied pages say September 21–26. Produce is supplied by **mnfood.club**; only the meats are branded **TC Farm**.
No earlier weeks or
“Have on hand” pantry items are included. No year was printed on the supplied pages.

- Demo page and recipe generation: `/demo/mnfood`
- Static ingredient JSON: `/demo/mnfood/ingredients`

The location works in both normal and mock mode and is deliberately absent from
nearby store search. Deploy the branch through the normal app deployment to make
these paths available on the public host. No separate inventory import, credentials,
or scraping is needed for this provider. Recipe generation still uses the app's
normal services.

The list combines all six share types, with duplicate produce consolidated and
share membership retained in categories. Blueberries retain their fruit substitution
label. Recommended add-ons are pork tenderloin, bratwurst or Italian sausage (two
choices), Ranger chicken thighs, sour cream, and Oddbird GSM NA dealcoholized red
wine. Add-ons are catalog options, not assertions that they come in the produce box.
The wine is also available to the existing wine pairing flow. Prices and package
sizes were not supplied and remain unset.

Inventory does not expire or change with the recipe date. Increment `Signature()`
when changing it so cached recipes and ingredients use the new catalog.

The demo represents Minneapolis–Saint Paul, the largest metro between Rochester
and St. Cloud, Minnesota. It uses the repository's downtown Minneapolis 55401 ZIP
centroid: latitude `44.985367`, longitude `-93.270208`. These coordinates also resolve
the local timezone; they do not identify a storefront or pickup address.
