# Round 9 mm-data report

## What changed
**Keys.** Every agent and workspace id in the app is `"<machineId>/<rawId>"` (`MachineKey`, RelayKit).
- Why a string key rather than a struct: every existing `[String: …]` map (selection, seen, archive, pending sends, controls caches, live replies, expansion sets, a11y ids) works unchanged, and views never had to learn about machines to stay correct.
- `NamespacedBackend` (RelayKit) wraps each machine's backend. It keys ids coming in (agents, workspaces, approvals, every WS event) and strips them going out. Bridges only ever see raw ids.
- Message and attachment ids stay raw. They always live under an agent key. The attachment and live-span caches are keyed by raw id: real bridge ids are random (16 hex / UUIDs) and the mock machines hand out disjoint ones.

**Pairings** (`PairingStore`, RelayKit): a list keyed by `/machine` id.
- App Group defaults key `machines` holds the JSON list (url, label, provisional, last `Machine`, last version). Each token sits in the Keychain under account = machine id.
- `upsert` replaces url and token for a known id and keeps its place and label. It also has `update`, `rekey`, `remove` and `removeAll`.
- Migration: the old `bridgeURL` + Keychain account = url becomes an entry under `provisional-legacy`. The old keys are deleted.
- On the first `/machine` that answers, AppStore re-keys the entry to the real id and moves the bare UI ids to `<id>/<raw>`: `selectedAgentId`, `seenAgents`, `archivedAgents`, `sidebarCompletedOpen`, `sidebarProjectsOpen`. `sessionFilter` isn't agent-keyed, so it's left alone.
- Tokens go through `TokenVault` (Keychain, or in-memory for tests).

**Store** (`AppStore` + new `MachineConnection`):
- One `MachineConnection` per machine. Each has its own socket task, backoff, generation counter, controls cache and status (`connecting` / `online` / `offline` / `needsRePair`).
- `refresh()` resyncs all machines in parallel. Each machine's agents and workspaces replace only its own, in pair order. An offline machine keeps its last known agents.
- Every refresh checks `/machine`. A changed id marks that machine `needsRePair` and merges nothing. A `401` marks only that machine.
- Status comes from the socket: online on `hello`; a drop from online reads `connecting`; a failed retry reads `offline`. Background resync failures of an unreachable machine are quiet (no banner).
- A provisional machine opens its socket only after it has been re-keyed.
- The default chat follows pair order, not whichever bridge answers first. "No agents" waits for every machine to have been tried.
- API for mm-ui (docs/tasks/round-9/interfaces.md):
  - reads: `machines: [MachineEntry]`, `machineFilter` (persisted; `sidebar` comes pre-filtered), `machineId(of:)`, `machine(of:)`, `isLive`, `rePairMachineId`;
  - actions: `addMachine(_:)` / `addMachine(link:)`, `rePair`, `rename`, `remove`, `kindControls(_:machineId:)`;
  - `store.backend` is now a router by key, so the uploads code works unchanged.
- The usage fan-out goes into mm-ui's `UsageStore` (`load(machineId:backend:)`, `apply(_:machineId:)`, `remove`, `setMachines`).
- `AppStore(backend:hostLabel:)` still exists: one unkeyed machine for the older unit tests and previews.

**App** (`AppModel`, `MainView`, `RelayApp`):
- `pair` adds a machine, or re-pairs `rePairMachineId`. A token is checked with `/machine` before anything is saved.
- `isPaired` is false once the last machine is removed, which brings back the first-launch screen.
- The re-pair banner names the machine when more than one is paired and opens PairingView as a sheet for that machine.
- `-pair … -agent …` still selects after the pairing lands, for the live UI tests.

**Mock** (`MockMachines.swift`, `MockBackend`):
- `-mock` is still one machine (`mock-mac`), so the old mock UI tests are unchanged.
- `-mockVM` adds `mock-vm` "Mock VM" (Ubuntu, desktop). It has workspace `w2` and pane `w2:p1` like the Mac, plus a working and a blocked chat.
- Test hooks: `-mockOffline <id>`, `-mockOnlineAfter <s>`, `-mockDrop <id> <s>` (connecting, then offline ~1 s later), `mock-*` pair links (token `bad` → 401), `-seedLegacyPairing <link>`, `-agent <key|raw>`.

## Tests
- New `RelayTests/MachinesTests.swift`, 18 tests:
  - keys and `NamespacedBackend` both ways;
  - legacy migration, upsert, rekey onto an existing id;
  - persisted-key migrate and drop;
  - same pane id on two machines routes to the right bridge;
  - an offline machine doesn't block the others and keeps its agents;
  - 401 and a changed id affect only that machine;
  - socket to status transitions;
  - provisional re-key online, and while offline;
  - add, re-pair (different machine throws), rename, remove;
  - an unreachable add saves nothing;
  - machine filter;
  - createAgent routes by workspace key;
  - a bare `-agent` resolves in pair order;
  - default selection follows pair order.
- `RelayTests` on iPhone 17: 168/168 pass, repeated runs green. One early run caught a flaky assertion in my own test (a duplicate fetch count), now fixed.

## How verified live (iPhone 17 simulator)
- `-seedLegacyPairing` with the real 7878 bridge. The list went from `provisional-legacy` to the Mac's real id with name, OS and version filled in, and the old `bridgeURL` key was gone. The chat loaded (read only).
- `-pair` to my own `second-bridge.sh` on 7882 (mm-qa had 7881). Both machines were listed, and the same herdr agents showed once per machine with their machine tag. The bridge was stopped afterwards (temp home removed), and the sim was reset to the Mac-only pairing.
- `-mock -mockVM -mockDrop mock-vm 3`: the VM rows grey out as offline and the Mac keeps working.

## What's left
- Done: MainView no longer passes `onUnpair`. SidebarView still has the defaulted param (mm-ui's file).
- ChatView drafts are `@State`, so they aren't persisted per agent. Nothing to migrate.
- If a legacy machine stays unreachable while another machine loads, the old bare selection is replaced by the other machine's first chat. Its seen and archive state still migrate later.

## Bugs fixed after the first report
- R9-3: the banner said "Reconnecting…" for a machine the menu showed as offline. `store.connectionNotice` now gives "<name> is offline" for an offline machine and "Reconnecting…" only while a dropped socket retries. Verified by mm-qa.
- R9-4: a migrated pairing whose bridge was down at launch never retried, because it has no socket until it's re-keyed. It now retries `/machine` with the socket backoff (1, 2, 4 … 30 s). Unit test added. Live check: a legacy pairing seeded to a stopped test bridge re-keyed within 40 s of the bridge starting, with no relaunch.
