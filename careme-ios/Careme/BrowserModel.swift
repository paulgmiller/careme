import Combine
import Foundation
import WebKit

@MainActor
final class BrowserModel: ObservableObject {
    @Published private(set) var currentURL = AppConfiguration.homeURL
    @Published private(set) var canGoBack = false
    @Published private(set) var canGoForward = false
    @Published private(set) var isLoading = false
    @Published private(set) var failureMessage: String?

    private weak var webView: WKWebView?

    func connect(_ webView: WKWebView) {
        self.webView = webView
        updateNavigationState(from: webView)
    }

    func updateNavigationState(from webView: WKWebView) {
        if let url = webView.url {
            currentURL = url
        }
        canGoBack = webView.canGoBack
        canGoForward = webView.canGoForward
        isLoading = webView.isLoading
    }

    func navigationStarted(in webView: WKWebView) {
        failureMessage = nil
        updateNavigationState(from: webView)
    }

    func navigationFinished(in webView: WKWebView) {
        failureMessage = nil
        updateNavigationState(from: webView)
    }

    func navigationFailed(in webView: WKWebView, error: Error) {
        updateNavigationState(from: webView)
        guard (error as NSError).code != NSURLErrorCancelled else { return }

        failureMessage = "Careme could not reach the kitchen. Check your connection and try again."
    }

    func goBack() {
        webView?.goBack()
    }

    func goForward() {
        webView?.goForward()
    }

    func reload() {
        failureMessage = nil
        guard let webView else { return }

        if webView.url == nil {
            webView.load(URLRequest(url: AppConfiguration.homeURL))
        } else {
            webView.reload()
        }
    }

    func loadCaremeURL(_ url: URL) {
        guard url.scheme == "https", url.host == AppConfiguration.homeURL.host else { return }
        failureMessage = nil
        webView?.load(URLRequest(url: url))
    }
}
