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

/// One paired machine as the app remembers it. The token isn't here; it lives in the Keychain under the machine id.
public struct MachineRecord: Codable, Hashable, Identifiable, Sendable {
    /// `GET /machine` id, or `PairingStore.provisionalId` until a migrated pairing has been reached once.
    public var id: String
    public var url: URL
    /// Local rename; nil shows the machine's own name.
    public var label: String?
    public var provisional: Bool
    /// Last `GET /machine`, so an offline machine still has a name and OS.
    public var machine: Machine?
    /// Last `GET /health` version.
    public var bridgeVersion: String?

    public init(id: String, url: URL, label: String? = nil, provisional: Bool = false, machine: Machine? = nil, bridgeVersion: String? = nil) {
        self.id = id
        self.url = url
        self.label = label
        self.provisional = provisional
        self.machine = machine
        self.bridgeVersion = bridgeVersion
    }
}

/// Where tokens live. The app uses the Keychain; tests use `.memory()`.
public struct TokenVault: Sendable {
    public var read: @Sendable (_ account: String) -> String?
    public var write: @Sendable (_ token: String, _ account: String) throws -> Void
    public var delete: @Sendable (_ account: String) -> Void

    public init(
        read: @escaping @Sendable (String) -> String?,
        write: @escaping @Sendable (String, String) throws -> Void,
        delete: @escaping @Sendable (String) -> Void
    ) {
        self.read = read
        self.write = write
        self.delete = delete
    }

    public static func keychain(service: String) -> TokenVault {
        TokenVault(
            read: { Keychain.read(service: service, account: $0) },
            write: { try Keychain.write($0, service: service, account: $1) },
            delete: { Keychain.delete(service: service, account: $0) }
        )
    }

    public static func memory() -> TokenVault {
        final class Box: @unchecked Sendable {
            let lock = NSLock()
            var values: [String: String] = [:]
        }
        let box = Box()
        return TokenVault(
            read: { account in box.lock.withLock { box.values[account] } },
            write: { token, account in box.lock.withLock { box.values[account] = token } },
            delete: { account in _ = box.lock.withLock { box.values.removeValue(forKey: account) } }
        )
    }
}

/// The paired machines, keyed by machine id (api.md "Multiple machines"): the list in the App Group's
/// defaults, each token in the Keychain (App Group access group) under the machine id, so extensions can read both.
public struct PairingStore: Sendable {
    public static let appGroup = "group.dev.amansingh.herd"
    static let service = "dev.amansingh.herd.bridge"
    static let listKey = "machines"
    /// The pre-round-9 single pairing: its URL here, its token under account = that URL.
    static let legacyURLKey = "bridgeURL"
    /// A migrated pairing whose bridge hasn't answered `/machine` yet.
    public static let provisionalId = "provisional-legacy"

    public static var sharedDefaults: UserDefaults { UserDefaults(suiteName: appGroup) ?? .standard }

    /// The app's real store.
    public static var shared: PairingStore { PairingStore(defaults: sharedDefaults, vault: .keychain(service: service)) }

    nonisolated(unsafe) let defaults: UserDefaults
    let vault: TokenVault

    public init(defaults: UserDefaults, vault: TokenVault) {
        self.defaults = defaults
        self.vault = vault
    }

    /// Every paired machine, in pair order. Moves the old single pairing into the list first.
    public func records() -> [MachineRecord] {
        migrateLegacy()
        return stored()
    }

    public func token(_ id: String) -> String? { vault.read(id) }

    public func pairing(_ id: String) -> Pairing? {
        guard let record = stored().first(where: { $0.id == id }), let token = token(id) else { return nil }
        return Pairing(url: record.url, token: token)
    }

    /// Adds a machine, or replaces the url and token of the one with the same id (keeping its place and label).
    public func upsert(_ record: MachineRecord, token: String) throws {
        try vault.write(token, record.id)
        var list = stored()
        if let i = list.firstIndex(where: { $0.id == record.id }) {
            var merged = record
            merged.label = record.label ?? list[i].label
            list[i] = merged
        } else {
            list.append(record)
        }
        save(list)
    }

    /// Metadata only (label, last machine, version). No-op for an unknown id.
    public func update(_ record: MachineRecord) {
        var list = stored()
        guard let i = list.firstIndex(where: { $0.id == record.id }) else { return }
        list[i] = record
        save(list)
    }

    /// Moves a provisional entry to its real id. If that id is already paired, the existing entry takes the
    /// provisional's url and token (the app never keeps two entries for one machine).
    public func rekey(_ oldId: String, to record: MachineRecord) throws {
        guard let token = token(oldId) else { return }
        try vault.write(token, record.id)
        if oldId != record.id { vault.delete(oldId) }
        var list = stored()
        let i = list.firstIndex { $0.id == oldId }
        if let existing = list.firstIndex(where: { $0.id == record.id }), existing != i {
            list[existing].url = record.url
            if let i { list.remove(at: i) }
        } else if let i {
            list[i] = record
        } else {
            list.append(record)
        }
        save(list)
    }

    public func remove(_ id: String) {
        vault.delete(id)
        save(stored().filter { $0.id != id })
    }

    public func removeAll() {
        for record in stored() { vault.delete(record.id) }
        defaults.removeObject(forKey: Self.listKey)
    }

    /// Old single pairing → list entry under `provisionalId`. Runs once; old keys go once the list is written.
    @discardableResult
    public func migrateLegacy() -> MachineRecord? {
        guard let s = defaults.string(forKey: Self.legacyURLKey) else { return nil }
        defer {
            vault.delete(s)
            defaults.removeObject(forKey: Self.legacyURLKey)
        }
        guard let url = URL(string: s), let token = vault.read(s) else { return nil }
        var list = stored()
        // A list that already has this URL wins (a crash between writing the list and deleting the old keys).
        if list.contains(where: { $0.url == url }) { return nil }
        let record = MachineRecord(id: Self.provisionalId, url: url, provisional: true)
        guard (try? vault.write(token, record.id)) != nil else { return nil }
        list.append(record)
        save(list)
        return record
    }

    /// Writes the pre-round-9 keys, for UI-testing the migration.
    public func writeLegacy(_ pairing: Pairing) throws {
        try vault.write(pairing.token, pairing.url.absoluteString)
        defaults.set(pairing.url.absoluteString, forKey: Self.legacyURLKey)
    }

    private func stored() -> [MachineRecord] {
        guard let data = defaults.data(forKey: Self.listKey) else { return [] }
        return (try? JSONDecoder().decode([MachineRecord].self, from: data)) ?? []
    }

    private func save(_ list: [MachineRecord]) {
        if let data = try? JSONEncoder().encode(list) { defaults.set(data, forKey: Self.listKey) }
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
