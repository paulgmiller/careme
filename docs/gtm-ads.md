# Google Tag Manager ad conversion setup

Careme loads a Google Tag Manager (GTM) web container when `GOOGLE_TAG_MANAGER_ID` is set. The app no longer sends Google Ads conversions directly with `gtag`; instead, it publishes neutral `dataLayer` events and expects GTM to fan those events out to ad platforms.

## Container ID

Set `GOOGLE_TAG_MANAGER_ID` to your GTM web container ID, for example `GTM-ABC1234`. Do not use a Google Ads ID like `AW-...` here; Google Ads conversion IDs and labels belong inside GTM tags.

## Signup conversion event

When `/auth/establish` detects a first-time user, the page pushes this event:

```js
window.dataLayer.push({
  event: "signup_completed",
  eventCallback: finishRedirect,
  eventTimeout: 1500,
});
```

Use a GTM custom event trigger named `signup_completed` for conversion tags.

## Google Ads conversion tag

In GTM, create a Google Ads conversion tag using the existing Google Ads conversion ID and label. Trigger it with the `signup_completed` custom event.

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
