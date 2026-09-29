# Round 9 interfaces: mm-data ↔ mm-ui (and mm-qa hooks)

Owner: mm-data. Status: **draft, pending mm-ui ack.** Changes go through a herdr message first.

## Keys
- App-wide key for anything per machine: `"<machineId>/<rawId>"`. Example `mock-vm/w2:p1`.
  - Used for `Agent.id`, `Agent.workspaceId`, `Workspace.id`, `Approval.agentId`, every `ServerEvent` agent id.
  - Machine ids never contain `/` (api.md, `[A-Za-z0-9._-]{1,64}`), so the first `/` splits it.
- Namespacing happens at the backend boundary (`NamespacedBackend` in RelayKit). Views never see a raw id; bridges never see a key.
- Helpers (RelayKit):
  ```swift
  public enum MachineKey {
      static func make(_ machineId: String, _ raw: String) -> String   // "m/w2:p1"
      static func machineId(_ key: String) -> String?                  // nil for a bare id
      static func raw(_ key: String) -> String                         // "w2:p1"
  }
  ```
- Message ids stay raw (always under an agent key). Attachment ids stay raw; `store.attachmentData(agentId:attachmentId:)` routes and caches by the agent's machine.

## AppStore (mm-data)
State the UI reads:
```swift
struct MachineEntry: Identifiable, Equatable {
    let id: String              // /machine id
    var displayName: String     // label ?? machine.name ?? url host
    var label: String?          // local rename; nil = none
    var machine: Machine?       // last /machine (name, kind, model, os)
    var bridgeVersion: String?  // /health version
    var herdrAvailable: Bool?   // /health herdr == "connected"; nil = unknown
    var host: String            // url host, for the Machines screen
    var status: MachineStatus
}
enum MachineStatus: Equatable { case connecting, online, offline, needsRePair }

store.machines: [MachineEntry]          // pair order, stable
store.machineFilter: String?            // nil = All; persisted ("machineFilter", AppDefaults.standard); reset to nil if that machine is removed
store.state: RelayState                 // one merged state, every machine; ids are keys
store.sidebar: SidebarModel             // state already filtered by machineFilter (Now + approvals span all machines in All)
store.machineId(of key: String) -> String?
store.machine(of key: String) -> MachineEntry?   // agent or workspace key
store.isLive(_ agentKey: String) -> Bool         // its machine is online
```
- Offline machine: its last-known agents and workspaces stay in `state`; `isLive` is false. Grey them.
- `needsRePair` machine: same as offline for its agents.
- Existing API keeps working with keys: `open`, `send`, `selectedAgentId`, `messages(for:)`, `setArchived`, `controlsState`, `control`, `interrupt`, `answer`, etc.
- `store.connection` / `store.needsRePairing` stay for the banner in MainView (mm-data): they describe the open chat's machine (or the filtered machine; in All with nothing open, any machine). The per-machine truth is `machines[i].status`.
- `store.backend` stays: a router that sends each per-agent call to the key's machine (ComposerAttachments keeps working). Global calls (`agents()`, `events()`) aren't meant for it.

Actions:
```swift
func addMachine(_ pairing: Pairing) async throws -> String   // GET /machine; appends, or replaces url+token if id known; returns machine id
func addMachine(link: URL) async throws -> String            // parses relay:// or herd:// link, then the above
func rePair(_ machineId: String, pairing: Pairing) async throws  // throws RelayError.differentMachine if /machine id differs
func rename(_ machineId: String, label: String?)             // nil or "" clears
func remove(_ machineId: String)                             // drops token, per-agent keys, agents, usage; the last one → pairing screen
func createAgent(workspaceId: String /*key*/, kind:, model:, effort:) async -> Bool   // machine from the key
func kindControls(_ kind: String, machineId: String) async -> AgentControlsInfo?
```
- Errors are `RelayError` with curated `errorDescription` (new cases: `.differentMachine`).
- `AppModel.pair(_ pairing:)` (first launch, PairingView) = `addMachine` plus creating the store if none. PairingView keeps working as is for Add machine.
- `AppModel.rePairMachineId: String?` — when set, PairingView's pair goes to `rePair` instead. (Or call `store.rePair` directly; your call.)

## UsageStore (mm-ui owns; agreed shape)
```swift
init()
func load(machineId: String, backend: any Backend) async   // AppStore calls on each machine's resync
func apply(_ provider: UsageProvider, machineId: String)   // AppStore forwards usage.updated
func remove(machineId: String)                             // AppStore on remove
func refresh() async                                       // fans out over the backends it was given in load
```
- The backend passed in is the machine's own (raw ids; usage has none anyway).
- Order: mm-ui lands this first and keeps the old `init(backend:)` + `load()` shims until mm-data's AppStore switch lands, then deletes them.

## New chat
- Machine choice is mm-ui's (last used in UI defaults). Pass that machine's workspace key to `createAgent`.

## Mock and test hooks (DEBUG)
- `-mock`: **one machine**, as before, so every existing mock UI test keeps working. Machine `mock-mac`, "Mock MacBook Pro", macOS 26.4, laptop.
- `-mockVM`: adds the second machine `mock-vm`, "Mock VM", `Ubuntu 24.04.1 LTS`, desktop.
  - Workspace `w2` "infra" (same raw id as the Mac's `w2` "website"; the sections must not merge).
  - Overlapping pane: `w2:p1`. Mac: "Landing page hero redesign" (claude, idle). VM: "Rotate TLS certificates" (claude, idle).
  - Also `w2:p2` "Nightly backup check" (codex, working) and `w5:p1` "Tune Postgres autovacuum" (claude, blocked, with an approval).
- `-mockOffline <machineId>`: that mock machine starts offline (REST throws unreachable, WS never connects).
- `-mockOnlineAfter <seconds>`: an offline mock machine comes back after that long.
- `-mockDrop <machineId> <seconds>`: that machine starts online and goes offline after that long (combine with `-mockOnlineAfter` on a relaunch if needed).
- In `-mock`, a pair link whose host starts with `mock-` adds a mock machine with no network: `relay://pair?url=http://mock-third:7878&token=t` → id `mock-third`, name "Mock Third". Token `bad` → 401 (`unauthorized`).
- `-seedLegacyPairing <relay link>` (DEBUG + `-uitest`): before AppModel init, clears the machine list and writes the old single-pairing keys (App Group `bridgeURL`, Keychain account = url). Then the normal migration runs.
- `-agent <key>` or `-agent <rawId>`. A raw id resolves to the first machine (pair order) that has it, once its agents load.
- Accessibility (suggestion to mm-ui): `session-<rawId>` when one machine is paired (old tests keep passing), `session-<key>` when more.

## Persistence (mm-data)
- Machine list: App Group defaults `machines` (JSON `[{id, url, label, provisional, machine, bridgeVersion}]`). Token in Keychain, service `dev.amansingh.herd.bridge`, account = machine id.
- Migration of the old pairing (`bridgeURL`, Keychain account = url): moved under provisional id `provisional-legacy`, re-keyed on the first `/machine` success. At that point bare ids in `selectedAgentId`, `seenAgents`, `archivedAgents`, `sidebarCompletedOpen`, `sidebarProjectsOpen` become `<id>/<raw>`. Old keys are removed once the list is written.
- Remove: drops the machine's keys from all of the above.
