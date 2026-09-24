import Foundation
import Observation
import RelayKit

/// Subscription usage for Claude and Codex/pi. No socket of its own: `AppStore` forwards
/// `usage.updated` frames from its one `/ws` connection into `apply(_:)`, and re-fetches the cache
/// on every resync (`load()`), so opening the Usage screen doesn't add a second WebSocket. See
/// api.md "Usage".
@MainActor
@Observable
final class UsageStore {
    let backend: any Backend

    private(set) var providers: [UsageProvider] = []
    private(set) var isLoading = false
    private(set) var errorMessage: String?

    init(backend: any Backend) {
        self.backend = backend
    }

    /// `GET /usage`: the bridge's cache. Called on first load and by `AppStore` on every resync.
    func load() async {
        isLoading = providers.isEmpty
        defer { isLoading = false }
        do {
            providers = try await backend.usage().providers
            errorMessage = nil
        } catch {
            errorMessage = (error as? RelayError)?.errorDescription ?? "Couldn't load usage."
        }
    }

    /// Pull-to-refresh. The bridge throttles this to once every 15 s; a `429` just means the cache
    /// is already as fresh as it's going to get, so it's treated like a normal reload, not an error.
    func refresh() async {
        do {
            try await backend.refreshUsage()
        } catch RelayError.http(429, _, _) {
            // Already refreshed recently.
        } catch {
            errorMessage = (error as? RelayError)?.errorDescription ?? "Couldn't refresh usage."
        }
        await load()
    }

    /// Forwarded by `AppStore` from `ServerEvent.usageUpdated`.
    func apply(_ provider: UsageProvider) {
        if let i = providers.firstIndex(where: { $0.id == provider.id }) {
            providers[i] = provider
        } else {
            providers.append(provider)
        }
    }
}
