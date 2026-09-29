import Foundation
import Observation
import RelayKit

/// Subscription usage for Claude and Codex/pi, across every paired machine. No socket of its own:
/// `AppStore` hands each machine's backend to `load(machineId:backend:)` on that machine's resync,
/// forwards `usage.updated` frames from that machine's `/ws` into `apply(_:machineId:)`, and keeps names
/// and order current with `setMachines`. Opening the Usage screen doesn't add a WebSocket. See api.md
/// "Usage" and "Multiple machines", and docs/tasks/round-9/interfaces.md.
@MainActor
@Observable
final class UsageStore {
    /// Machine ids and display names, in pairing order.
    private(set) var machines: [(id: String, name: String)] = []
    /// `GET /usage` providers per machine id.
    private(set) var byMachine: [String: [UsageProvider]] = [:]
    /// The last load or refresh error per machine id.
    private(set) var errors: [String: String] = [:]
    @ObservationIgnored private var backends: [String: any Backend] = [:]
    private var loading: Set<String> = []

    init() {}

    /// A single-machine store (tests and previews). The machine id is empty.
    convenience init(backend: any Backend) {
        self.init()
        backends[""] = backend
        machines = [("", "")]
    }

    /// Names and pairing order from `AppStore.machines`; call it whenever that list changes (rename
    /// included). Machines missing from the list are forgotten.
    func setMachines(_ list: [(id: String, name: String)]) {
        for id in machines.map(\.id) where !list.contains(where: { $0.id == id }) { remove(machineId: id) }
        machines = list
    }

    /// Forgets a machine and its usage.
    func remove(machineId: String) {
        machines.removeAll { $0.id == machineId }
        backends[machineId] = nil
        byMachine[machineId] = nil
        errors[machineId] = nil
        loading.remove(machineId)
    }

    /// Every provider of every machine, merged where it's the same subscription.
    var cards: [UsageCard] { UsageCard.merge(machines, byMachine) }

    /// Machine names only mean something with more than one machine paired.
    var showsMachines: Bool { machines.count > 1 }

    var isLoading: Bool { !loading.isEmpty && byMachine.values.allSatisfy(\.isEmpty) }

    /// For the empty state: the first machine's error, prefixed with its name when there are several.
    var errorMessage: String? {
        for m in machines {
            if let error = errors[m.id] { return showsMachines ? "\(m.name): \(error)" : error }
        }
        return nil
    }

    /// Every provider, unmerged (single-machine callers and tests).
    var providers: [UsageProvider] { machines.flatMap { byMachine[$0.id] ?? [] } }

    /// `GET /usage` on every machine at once. A slow or offline machine never holds up the others:
    /// each one's answer lands as soon as it arrives.
    func load() async {
        await withTaskGroup(of: Void.self) { group in
            for (id, backend) in backends {
                group.addTask { await self.load(machineId: id, backend: backend) }
            }
        }
    }

    /// `GET /usage` on one machine: the bridge's cache. `AppStore` calls this on that machine's resync,
    /// with that machine's own backend.
    func load(machineId: String, backend: any Backend) async {
        backends[machineId] = backend
        if !machines.contains(where: { $0.id == machineId }) { machines.append((machineId, "")) }
        loading.insert(machineId)
        defer { loading.remove(machineId) }
        do {
            let providers = try await backend.usage().providers
            guard backends[machineId] != nil else { return }
            byMachine[machineId] = providers
            errors[machineId] = nil
        } catch {
            guard backends[machineId] != nil else { return }
            errors[machineId] = (error as? RelayError)?.errorDescription ?? "Couldn't load usage."
        }
    }

    /// Pull-to-refresh on every machine. Each bridge throttles this to once every 15 s; a `429` just
    /// means that cache is already as fresh as it's going to get, so it's a normal reload, not an error.
    func refresh() async {
        await withTaskGroup(of: Void.self) { group in
            for (id, backend) in backends {
                group.addTask { await self.refresh(machineId: id, backend: backend) }
            }
        }
    }

    private func refresh(machineId: String, backend: any Backend) async {
        do {
            try await backend.refreshUsage()
        } catch RelayError.http(429, _, _) {
            // Already refreshed recently.
        } catch {
            errors[machineId] = (error as? RelayError)?.errorDescription ?? "Couldn't refresh usage."
        }
        await load(machineId: machineId, backend: backend)
    }

    /// Forwarded by `AppStore` from that machine's `ServerEvent.usageUpdated`.
    func apply(_ provider: UsageProvider, machineId: String) {
        guard backends[machineId] != nil else { return }
        var list = byMachine[machineId] ?? []
        if let i = list.firstIndex(where: { $0.id == provider.id }) {
            list[i] = provider
        } else {
            list.append(provider)
        }
        byMachine[machineId] = list
    }

    /// Single-machine form of `apply(_:machineId:)`.
    func apply(_ provider: UsageProvider) {
        apply(provider, machineId: machines.first?.id ?? "")
    }
}

/// One card on the Usage screen: a subscription, on one machine or on several that share it.
struct UsageCard: Identifiable, Equatable {
    /// The provider id plus its machine ids, so a card that splits or merges gets a new identity.
    var id: String
    /// The freshest snapshot among the machines; its windows are what the card shows.
    var provider: UsageProvider
    /// The machines drawing on this subscription, in pairing order.
    var machineIds: [String]
    var machineNames: [String]

    /// Bridges don't report an account, so "same subscription" is a guess (api.md "Multiple machines"):
    /// same provider id and plan, and, where both have numbers, the same windows at about the same use and
    /// reset time. Two accounts on the same plan almost never agree on all of that; when in doubt the cards
    /// stay apart, one per machine.
    static func sameSubscription(_ a: UsageProvider, _ b: UsageProvider) -> Bool {
        guard a.id == b.id, let plan = a.plan, plan == b.plan else { return false }
        if a.windows.isEmpty && b.windows.isEmpty { return a.stale == b.stale }
        guard Set(a.windows.map(\.id)) == Set(b.windows.map(\.id)) else { return false }
        for wa in a.windows {
            guard let wb = b.windows.first(where: { $0.id == wa.id }) else { return false }
            switch (wa.usedPercent, wb.usedPercent) {
            case let (pa?, pb?): if abs(pa - pb) > 2 { return false }
            case (nil, nil): break
            default: return false
            }
            if let ra = wa.resetsAt, let rb = wb.resetsAt, abs(ra.timeIntervalSince(rb)) > 300 { return false }
        }
        return true
    }

    /// Cards in machine order, then each machine's provider order. A machine joins an existing card when
    /// its provider looks like the same subscription as that card's.
    static func merge(_ machines: [(id: String, name: String)], _ byMachine: [String: [UsageProvider]]) -> [UsageCard] {
        var cards: [UsageCard] = []
        for machine in machines {
            for provider in byMachine[machine.id] ?? [] {
                if let i = cards.firstIndex(where: {
                    !$0.machineIds.contains(machine.id) && sameSubscription($0.provider, provider)
                }) {
                    var card = cards[i]
                    card.machineIds.append(machine.id)
                    card.machineNames.append(machine.name)
                    if provider.updatedAt > card.provider.updatedAt {
                        let usedBy = card.provider.usedBy
                        card.provider = provider
                        card.provider.usedBy = usedBy
                    }
                    for kind in provider.usedBy where !card.provider.usedBy.contains(kind) {
                        card.provider.usedBy.append(kind)
                    }
                    card.id = "\(provider.id)|\(card.machineIds.joined(separator: ","))"
                    cards[i] = card
                } else {
                    cards.append(UsageCard(
                        id: "\(provider.id)|\(machine.id)", provider: provider,
                        machineIds: [machine.id], machineNames: [machine.name]
                    ))
                }
            }
        }
        return cards
    }
}
