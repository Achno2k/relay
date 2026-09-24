import Foundation
import Security

/// Bridge URL + token.
public struct Pairing: Hashable, Sendable {
    public var url: URL
    public var token: String

    public init(url: URL, token: String) {
        self.url = url
        self.token = token
    }

    /// `relay` since the rename; `herd` so old QR codes and links still pair.
    public static let schemes: Set<String> = ["relay", "herd"]

    /// Parses `relay://pair?url=<base>&token=<token>` (or the old `herd://`).
    public init?(link: URL) {
        guard let comps = URLComponents(url: link, resolvingAgainstBaseURL: false),
              let scheme = comps.scheme?.lowercased(), Self.schemes.contains(scheme),
              comps.host?.lowercased() == "pair",
              let urlString = comps.queryItems?.first(where: { $0.name == "url" })?.value,
              let token = comps.queryItems?.first(where: { $0.name == "token" })?.value,
              !token.isEmpty,
              let url = Self.baseURL(from: urlString)
        else { return nil }
        self.init(url: url, token: token)
    }

    public init?(linkString: String) {
        guard let url = URL(string: linkString.trimmingCharacters(in: .whitespacesAndNewlines)) else { return nil }
        self.init(link: url)
    }

    /// Accepts `100.64.0.1:7878` as well as full URLs.
    public static func baseURL(from string: String) -> URL? {
        var s = string.trimmingCharacters(in: .whitespacesAndNewlines)
        if s.isEmpty { return nil }
        if !s.contains("://") { s = "http://" + s }
        guard let url = URL(string: s), let scheme = url.scheme?.lowercased(),
              scheme == "http" || scheme == "https", url.host != nil
        else { return nil }
        return url
    }
}

/// Stores the pairing where app extensions can read it: the token in the Keychain under the
/// App Group access group, the URL in the App Group's defaults.
public enum PairingStore {
    public static let appGroup = "group.dev.amansingh.herd"
    static let service = "dev.amansingh.herd.bridge"
    static let urlKey = "bridgeURL"

    public static var sharedDefaults: UserDefaults { UserDefaults(suiteName: appGroup) ?? .standard }

    public static func load() -> Pairing? {
        guard let s = sharedDefaults.string(forKey: urlKey), let url = URL(string: s),
              let token = Keychain.read(service: service, account: s)
        else { return nil }
        return Pairing(url: url, token: token)
    }

    public static func save(_ pairing: Pairing) throws {
        clear()
        try Keychain.write(pairing.token, service: service, account: pairing.url.absoluteString)
        sharedDefaults.set(pairing.url.absoluteString, forKey: urlKey)
    }

    public static func clear() {
        if let s = sharedDefaults.string(forKey: urlKey) {
            Keychain.delete(service: service, account: s)
        }
        sharedDefaults.removeObject(forKey: urlKey)
    }
}

public enum Keychain {
    public struct Failure: Error, Sendable {
        public let status: OSStatus
    }

    /// Unsigned simulator builds lack the access-group entitlement (errSecMissingEntitlement),
    /// so every call retries without the group.
    static let accessGroup: String? = PairingStore.appGroup

    public static func write(_ value: String, service: String, account: String) throws {
        delete(service: service, account: account)
        var status = SecItemAdd(addQuery(value, service, account, group: accessGroup) as CFDictionary, nil)
        if status == errSecMissingEntitlement {
            status = SecItemAdd(addQuery(value, service, account, group: nil) as CFDictionary, nil)
        }
        guard status == errSecSuccess else { throw Failure(status: status) }
    }

    public static func read(service: String, account: String) -> String? {
        for group in [accessGroup, nil] {
            var query = base(service, account, group: group)
            query[kSecReturnData as String] = true
            query[kSecMatchLimit as String] = kSecMatchLimitOne
            var result: AnyObject?
            if SecItemCopyMatching(query as CFDictionary, &result) == errSecSuccess,
               let data = result as? Data {
                return String(decoding: data, as: UTF8.self)
            }
        }
        return nil
    }

    public static func delete(service: String, account: String) {
        for group in [accessGroup, nil] {
            SecItemDelete(base(service, account, group: group) as CFDictionary)
        }
    }

    private static func base(_ service: String, _ account: String, group: String?) -> [String: Any] {
        var q: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: account,
        ]
        if let group { q[kSecAttrAccessGroup as String] = group }
        return q
    }

    private static func addQuery(_ value: String, _ service: String, _ account: String, group: String?) -> [String: Any] {
        var q = base(service, account, group: group)
        q[kSecValueData as String] = Data(value.utf8)
        q[kSecAttrAccessible as String] = kSecAttrAccessibleAfterFirstUnlock
        return q
    }
}
