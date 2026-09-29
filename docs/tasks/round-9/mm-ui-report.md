# Round 9 mm-ui report

## What changed
- **Machine menu** (`SidebarToolbar.swift`)
  - Capsule shows "All machines" or the machine's name, with a status dot: filled online, hollow connecting, grey offline, orange re-pair. In All it shows the worst status.
  - Menu: an inline Picker (All + one item per machine, checkmark on the current one, status symbol), then "Add machine…" and "Manage machines…". Menu pickers drop subtitles, so a machine that isn't online is titled "Mock VM · Offline".
  - ••• menu: Usage, Machines. "Unpair" is gone; Remove on the Machines screen replaces it per machine.
- **Sidebar**
  - Sections are keyed by workspace key, so two machines' projects never merge (tested with the same raw `w1`).
  - Project headers and the all-in-Now row carry a small machine tag, only in All with more than one machine paired (`MachineTag`).
  - Two-line rows (Now, filters, search) add the machine to the subtitle in All. The project and machine truncate; the age always shows.
  - Rows of an offline or re-pair machine are greyed, with a struck circle glyph and "offline" in place of the age. VoiceOver value starts with "offline". Stop is disabled.
  - A down machine with no chats listed gets a notice card ("Mock VM · Offline. Its chats show here once it's back."), which opens Machines (`SidebarMachineNotices.swift`).
  - New chat from the bottom bar in All with several machines passes no folder, so the sheet starts on the last used machine.
- **Machines screen** (`Features/Machines/MachinesView.swift`), from the machine menu and •••
  - List: kind icon, name, dot, "Online · macOS 26.4 · relay 0.9.0".
  - Detail: name field (local label; empty = the machine's own name), status, herdr, system, model, bridge, address; Re-pair…; Remove Machine with a confirm dialog. A re-pair-needed machine puts Re-pair first. After Remove it pops back to the list.
  - + adds a machine.
- **Pairing** (`PairingView.swift`): one view, three modes. It works out the mode itself: re-pair when `AppModel.rePairMachineId` is set, add when presented as a sheet, otherwise first launch. Copy changes per mode; as a sheet it has a close button and closes itself once the bridge answers.
- **New chat** (`NewChatSheet.swift`): a Machine picker in All with more than one machine. Folders filter to that machine. Model/effort load from that machine (`kindControls(_:machineId:)`). Default: the project's machine (its +), else the filtered machine, else the last used (`newChatMachine`), else the open chat's, else the first online. Create is disabled while the chosen machine is down.
- **Usage** (`UsageStore.swift`, `UsageView.swift`): per-machine store (`load(machineId:backend:)`, `apply(_:machineId:)`, `remove(machineId:)`, `setMachines`, `refresh()` fans out). `UsageCard.merge` joins machines only when provider id and plan match and the windows agree (use within 2 points, resets within 5 min). Otherwise one card per machine. Cards list their machines when more than one is paired.
- `StatusGlyph` has an `offline` variant.
- `SidebarModel+Machines.swift`: `showsMachineTags`, `machineTag(_:)`, `filteredMachine`, `accessibilityKey(_:)`, and `MachineStatus` copy and symbols.

## Accessibility ids (agreed with mm-qa)
- `sidebarMachineMenu` (label "Machine: All machines" / "Machine: <name>", value = status), `machineMenuAll`, `machineMenuItem-<id>` (label "Machine: <name>, <status>"), `machineMenuAdd`, `machineMenuManage`, `sidebarMachines`.
- Machines: `machineRow-<id>`, `machineNameField`, `machineRePair`, `machineRemove`, `machineRemoveConfirm`, `machinesAdd`, `machinesDone`.
- `sectionMachineTag` (label = machine name), `machineNotice-<id>`, `newChatMachine`.
- `session-<rawId>` with one machine, `session-<key>` with more.
- Pairing: `pairingScan`, `pairingManual`, `pairingURL`, `pairingToken`, `pairingConnect`, `pairingCancel`, `pairingError`.

## Tests
- `UsageStoreTests.swift`: new suite "Usage across machines" (merge rules, per-machine load/apply/remove, one machine failing).
- `SidebarMachinesTests.swift`: new-chat default machine, status copy, two machines' same project id stay two sections, tags only in All with several machines, row id keys.
- `SidebarShellUITests.testToolbarMenus`: now checks for Add machine instead of the old "Connected" row.

## How verified
- Full `RelayTests` on iPhone 17: 168 tests, all pass.
- Mock screenshots on iPhone 17: `-mock -mockVM` (tags, subtitles, usage merge), `-mockOffline mock-vm` with and without `-machineFilter mock-vm` (notice card, grey dot), `-demo machines`, `-demo newChat`, `-demo usage`.
- A throwaway UI test (deleted, never committed) on a private simulator, during mm-qa's slot, walked the flow: open the machine menu, pick Mock VM, Manage machines, the VM's detail, Remove, confirm, back at the list with the VM gone and the tags gone, then Add machine. The shared iPhone 17 was busy with mm-data's runs. `SidebarShellUITests.testToolbarMenus` passes with the new menu.
- Found on the way, fixed: menu status subtitles didn't render (moved into the title); a double dismiss after Remove closed the whole Machines sheet.
- Told mm-qa: with `-mockVM` the default chat can be the blocked VM agent, whose approval sheet blocks toolbar taps under `-demo sidebar`; launch with an idle `-agent`. The remove-confirm button matches twice in XCUI (`firstMatch`).

## Notes
- `SidebarShellUITests.swift` isn't in anyone's round 9 paths. I changed one assertion there because it tested the menu this round replaced.
- Commits: `e6f0cf2` (usage), `05054b8` (UI).

## What's left
- None open on the UI side.
- Live check against 7878 + `second-bridge.sh` is mm-qa's.
