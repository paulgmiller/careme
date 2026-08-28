# Careme iOS shell

This directory contains a dependency-free SwiftUI shell for `https://careme.cooking`. Open `Careme.xcodeproj` with Xcode 26 or newer and run the `Careme` scheme.

The shell currently provides persistent web sessions, native back/forward/reload controls, the iOS share sheet, an explicit Safari action, loading feedback, recoverable navigation errors, and a versioned `Careme-iOS/<version>` user-agent suffix. HTTPS authentication pages remain in the web view so Clerk's hosted flow can return to Careme.

## Before the first archive

1. Select the Careme target and choose the North Briton LLC development team under Signing & Capabilities.
2. Confirm that `cooking.careme` is the intended bundle identifier.
3. Replace the empty AppIcon asset with the final 1024-by-1024 icon. The project intentionally does not upscale the 512-pixel web icon.
4. Add Associated Domains after `apple-app-site-association` is deployed on `careme.cooking`.
5. Confirm that the App Store-required Xcode/iOS SDK versions have not changed.

## Testing without Apple hardware

There are useful layers of testing, but Linux cannot run Xcode or Apple's iOS Simulator:

- **GitHub Actions:** `.github/workflows/ios.yml` builds the app for a generic iOS Simulator on a [hosted macOS runner](https://docs.github.com/en/actions/reference/runners/github-hosted-runners). This catches Xcode project, Swift compiler, asset-catalog, and linkage failures on every relevant change. It does not provide an interactive screen.
- **Rented Mac:** a short-lived hosted Mac from a macOS cloud provider gives you Xcode and Simulator without buying a Mac. Do not put long-lived signing certificates or unencrypted App Store credentials on a machine you do not control; prefer App Store Connect API keys with limited roles and remove them afterward.
- **Remote browser/device services:** these can help with Safari layout and, when they explicitly support uploaded `.ipa` files, interaction with a signed build. They are supplementary; confirm current iOS version, real-device versus simulator status, privacy, and pricing before uploading a production-signed app.
- **The existing PWA:** browser responsive mode and automated web tests remain the fastest way to exercise Careme's HTML flows. They do not test `WKWebView`, iOS permission prompts, cookie behavior, universal links, StoreKit, or app lifecycle transitions.

The cheapest credible path is GitHub Actions for continuous compile checks plus a few hours on a rented Mac for interactive Simulator testing. Before App Store submission, recruit at least one person with an iPhone for TestFlight. [Apple notes that Simulator does not reproduce every hardware behavior](https://developer.apple.com/documentation/xcode/running-your-app-on-simulated-or-physical-devices), so the physical-device pass should cover Clerk authentication, location permission and fallback, photo selection/camera, share sheet, offline recovery, background/foreground transitions, and session persistence.

## Command used by CI

```sh
xcodebuild \
  -project careme-ios/Careme.xcodeproj \
  -scheme Careme \
  -configuration Debug \
  -destination 'generic/platform=iOS Simulator' \
  CODE_SIGNING_ALLOWED=NO \
  build
```

An App Store archive and upload still require Apple signing credentials and should only be added to CI after the developer membership, App ID, and secrets-handling policy are settled.
