# Careme iOS and App Store plan

## Goal

Ship Careme in the iPhone App Store without duplicating the Go application. The iOS app will be a small, dependency-free SwiftUI shell around `https://careme.cooking`, with native navigation and platform integrations. The website remains the source of truth for account data, recipes, shopping lists, and subscriptions.

The first release should be a free companion app. It will recognize existing subscriber entitlements but will not display Clerk checkout, web pricing, or links that ask an iOS user to purchase outside the app. Selling subscriptions inside the iOS app is a later StoreKit phase.

## Delivery phases

### 1. Native shell

- Maintain a standard Xcode project in `careme-ios/`, targeting iPhone on iOS 17 or later and built with the current App Store-required Xcode and iOS SDK.
- Load Careme in a persistent `WKWebView` so Clerk sessions and site preferences survive launches.
- Add native back, forward, reload, share, and open-in-browser actions, plus loading and recoverable connection-error states.
- Append `Careme-iOS/<version>` to the user agent. Use that marker on the Go server for iOS-only policy and presentation decisions; do not depend on generic iPhone user-agent detection.
- Keep all HTTPS navigation in the web view so Clerk's hosted authentication flow can complete. Send non-HTTP schemes to the operating system.
- Add universal links after the Apple Team ID is known: serve `/.well-known/apple-app-site-association`, add the Associated Domains entitlement, and route Careme links into the existing web view.

### 2. App Review compliance

- Replace the email-only deletion instructions with an authenticated in-app deletion action. It must delete or de-identify Careme data, request deletion of the Clerk identity, revoke the session, and report a definite success or contextual error.
- When the `Careme-iOS` marker is present, hide Clerk's pricing table and every external purchase call to action. Existing subscribers may sign in and use their entitlement.
- Audit the enabled Clerk providers. If any social provider such as Google is enabled, add Sign in with Apple as an equivalent login method.
- Disable Clarity and advertising/conversion tags in the iOS shell unless they are explicitly approved for the app. If retained, assess App Tracking Transparency and declare their data practices.
- Update the privacy policy to name the iOS app and describe direct account deletion. Complete App Store privacy answers for Careme and every service used by the app.
- Preserve ZIP-code entry when location permission is denied. Add clear location, camera, and photo-selection purpose strings.

### 3. Store preparation and submission

- Enroll North Briton LLC in the Apple Developer Program, obtain or confirm its D-U-N-S record, and create the `cooking.careme` App ID and App Store Connect record.
- Produce a source-quality 1024-by-1024 icon without transparency and populate the Xcode asset catalog. The current 512-pixel web icon is a placeholder source, not the final App Store asset.
- Prepare App Store copy, support and privacy URLs, age-rating and export-compliance answers, category, territories, and four to six current iPhone screenshots.
- Create a reviewer account with full feature access and review notes explaining authentication, location fallback, offline recipes, and why the free companion has no purchase UI.
- Run automated simulator builds, exercise the app interactively in Simulator, then perform a short TestFlight round before submitting for review.

### 4. Optional StoreKit subscriptions

- Create an auto-renewable subscription group and matching products in App Store Connect.
- Add native purchase, restore, and manage-subscription experiences using StoreKit.
- Verify signed transactions on the server, consume App Store Server Notifications, and map Apple subscription state into the same entitlement used by web/Clerk subscribers.
- Test new purchase, renewal, expiration, cancellation, billing retry, refund/revocation, restore, and cross-platform account-linking cases before exposing the products.

## Acceptance checks

- Fresh install, relaunch, cookie persistence, sign-in, sign-out, recipe generation, save/remove recipe, shopping list, sharing, and offline/error recovery all work in Simulator and TestFlight.
- External authentication returns to an authenticated Careme session without opening an untrusted URL as app content.
- Denying location still leaves ZIP lookup usable; granting it successfully finds nearby stores.
- The iOS experience contains no Clerk pricing table, external checkout link, or conversion tracker in the first release.
- A signed-in user can permanently delete the account in the app and cannot use the old session afterward.
- The reviewer account, backend services, privacy URL, support URL, screenshots, and review notes are valid at submission time.

## Decisions and open dependencies

- **Chosen:** native SwiftUI plus `WKWebView`, no Capacitor or third-party iOS dependencies.
- **Chosen:** iPhone-only first release to reduce layout and screenshot scope.
- **Chosen:** free companion first; StoreKit sales are a separate phase.
- **Pending external information:** Apple Team ID, final signing team, App Store listing identifiers, production Clerk provider configuration, and the final 1024-pixel icon.
- **Required before release:** a Mac environment with Xcode for interactive Simulator work, signing, archive validation, and upload. A physical iPhone is strongly recommended for the final location, camera/photo, authentication, and offline checks.
