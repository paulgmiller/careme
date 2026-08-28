import Foundation

enum AppConfiguration {
    static let homeURL = URL(string: "https://careme.cooking/")!

    static var userAgentSuffix: String {
        let version = Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String
        return "Careme-iOS/\(version ?? "unknown")"
    }
}
