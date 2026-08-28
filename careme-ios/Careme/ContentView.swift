import SwiftUI
import UIKit

struct ContentView: View {
    @StateObject private var browser = BrowserModel()

    var body: some View {
        WebView(model: browser)
            .ignoresSafeArea(.container, edges: .bottom)
            .overlay(alignment: .top) {
                if browser.isLoading {
                    ProgressView()
                        .progressViewStyle(.linear)
                        .tint(Color(red: 0.25, green: 0.42, blue: 0.31))
                }
            }
            .overlay {
                if let failureMessage = browser.failureMessage {
                    connectionError(message: failureMessage)
                }
            }
            .safeAreaInset(edge: .bottom, spacing: 0) {
                browserToolbar
            }
            .onOpenURL(perform: browser.loadCaremeURL)
            .tint(Color(red: 0.25, green: 0.42, blue: 0.31))
    }

    private var browserToolbar: some View {
        HStack(spacing: 24) {
            Button("Back", systemImage: "chevron.backward", action: browser.goBack)
                .disabled(!browser.canGoBack)
            Button("Forward", systemImage: "chevron.forward", action: browser.goForward)
                .disabled(!browser.canGoForward)
            Button("Reload", systemImage: "arrow.clockwise", action: browser.reload)
            Spacer()
            ShareLink(item: browser.currentURL) {
                Label("Share", systemImage: "square.and.arrow.up")
            }
            Button("Open in Safari", systemImage: "safari") {
                UIApplication.shared.open(browser.currentURL)
            }
        }
        .labelStyle(.iconOnly)
        .font(.title3)
        .padding(.horizontal, 20)
        .padding(.vertical, 12)
        .background(.bar)
        .overlay(alignment: .top) {
            Divider()
        }
    }

    private func connectionError(message: String) -> some View {
        VStack(spacing: 14) {
            Image(systemName: "wifi.exclamationmark")
                .font(.system(size: 36))
                .foregroundStyle(.secondary)
            Text("The kitchen is out of reach")
                .font(.headline)
            Text(message)
                .font(.subheadline)
                .foregroundStyle(.secondary)
                .multilineTextAlignment(.center)
            Button("Try again, chef", action: browser.reload)
                .buttonStyle(.borderedProminent)
        }
        .padding(24)
        .frame(maxWidth: 340)
        .background(.regularMaterial, in: RoundedRectangle(cornerRadius: 20))
        .padding()
    }
}
