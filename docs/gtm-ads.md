# Google Tag Manager ad conversion setup

Careme loads a Google Tag Manager (GTM) web container when `GOOGLE_TAG_MANAGER_ID` is set. The app no longer sends Google Ads conversions directly with `gtag`; instead, it publishes neutral `dataLayer` events and expects GTM to fan those events out to ad platforms.

## Container ID

Set `GOOGLE_TAG_MANAGER_ID` to your GTM web container ID, for example `GTM-KP55TPW6`. Do not use a Google Ads ID like `AW-...` here; Google Ads conversion IDs and labels belong inside GTM tags.

## Conversion events

Careme publishes neutral custom events to `window.dataLayer` only after the corresponding server action succeeds:

| Event | When it fires |
| --- | --- |
| `signup_completed` | A first-time user finishes signing up. |
| `recipe_generation` | A newly generated recipe list finishes and is shown to the cook. |
| `recipe_save` | A recipe is saved, including a save completed after signing in. |

The destination page removes its one-time conversion query parameter with `history.replaceState` before publishing the event. This prevents a refresh from counting the same signup, generation, or post-sign-in save again. The argument is repeatable when one request completes multiple conversions, such as a new signup that immediately saves a recipe.

### Signup

When `/auth/establish` detects a first-time user, it adds the conversion to the local return destination:

```js
destination.searchParams.set("conversion", "signup_completed");
location.replace(destination);
```

The shared application head consumes the query argument on the destination, publishes `signup_completed` to `window.dataLayer`, and removes the argument from the visible URL. The app renders both the GTM head script and the GTM `noscript` iframe fallback immediately after the opening `<body>` tag when this environment variable is set.

### Recipe generation and saves

Successful recipe generations and saves publish these payloads:

```js
window.dataLayer.push({ event: "recipe_generation" });
window.dataLayer.push({ event: "recipe_save" });
```

In GTM, create Custom Event triggers named `recipe_generation` and `recipe_save` and attach them to the appropriate conversion tags.

## Google Ads conversion tag

In GTM, create a Google Ads conversion tag using the relevant Google Ads conversion ID and label. Trigger it with the matching custom event from the table above. Use a separate tag when the Google Ads conversion action or value differs by event.

## OpenAI / ChatGPT Ads pixel

To keep application JavaScript small, install the OpenAI Ads Measurement Pixel inside GTM rather than in Careme templates:

1. Create a Custom HTML tag in GTM for the OpenAI pixel installation snippet.
2. Put the OpenAI Ads pixel ID from Ads Manager in that snippet.
3. Fire the installation tag on the pages where conversions can happen, or on all pages if you want the pixel loaded broadly.
4. Create a second Custom HTML tag triggered by `signup_completed` that calls the OpenAI standard event for registration completion:

```js
oaiq("measure", "registration_completed", {
  type: "customer_action",
});
```

The browser still executes OpenAI's JavaScript SDK, but it is served and managed through GTM rather than committed to the app.
