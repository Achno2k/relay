# Round 2: user feedback from the phone

The user is running the app on their iPhone 13 (dark mode). The bridge is now the LaunchAgent `com.herd.bridge` on 7878. After a bridge change:
- `swift build -c release`
- `launchctl kickstart -k gui/$(id -u)/com.herd.bridge`

Don't run another bridge on 7878. For isolated testing use `--local-only --port 7979`.

Reference screenshots (user-provided) are copied to `docs/screenshots/ref/`:
- `current-sidebar.png`: what they have now; too cluttered.
- `codex-home.png`: the target sidebar/home structure.
- `filter-menu.png`: the target filter menu and session rows.

## 1. Scroll-to-bottom button does nothing (iOS)
Tapping the floating ↓ button doesn't scroll. Fix it, and make it animate to the true bottom (the last message plus the working indicator). Add it to a mock UI test: scroll up, tap, assert the button disappears and the last message is hittable.

## 2. Attachments from the `+` button (bridge + iOS)
- **iOS**:
  - `+` opens a menu: **Photos** (PhotosPicker, multi-select), **Camera**, **Files** (`fileImporter`: images, PDF, plain text/code, any data).
  - Selected items show as removable chips/thumbnails above the text field inside the glass composer, as in ChatGPT.
  - Images are downscaled to at most 2048 px and sent as JPEG (HEIC → JPEG). Other files are sent as they are.
  - Limit is 20 MB per file and 10 files per message.
  - Upload progress on each chip. Send is enabled once uploads finish.
- **Bridge**:
  - `POST /agents/:id/attachments` takes a raw body with `Content-Type` and `X-Filename` headers and returns `201 {"id","name","kind":"image|pdf|file","size"}`.
  - It stores the file under `~/.herd/uploads/<agentPaneId-safe>/<id>-<sanitized name>` (0600).
  - `POST /agents/:id/prompt` accepts `"attachments":[id…]`. The bridge appends the absolute paths to the prompt text in a fixed marker format Claude understands, e.g. a final line `Attached files: /abs/a.jpg /abs/b.pdf`. Claude reads images and PDFs from a path.
  - Absolute paths never cross the wire. When parsing the transcript, the bridge recognises its own marker and turns it into `{"type":"attachment","id","name","kind"}` blocks, with no path, instead of text.
  - `GET /agents/:id/attachments/:id` serves the file back so the app can show thumbnails in history.
  - Uploads are cleaned up after 7 days (on boot plus daily).
  - Document all of this in api.md first.
- **iOS**: user bubbles show image thumbnails (tap for full screen) and file chips (tap for QuickLook) above the text.
- **Live test**: attach a small generated PNG containing the text "HERD" and ask the agent what text is in the image. Assert the reply contains HERD. Also run one with a PDF.

## 3. Sidebar rework (iOS, small bridge addition)
Target structure: `codex-home.png`, restyled in our Liquid Glass look, dark and light.
- Large title **Herd** at the top, plus a `⋯` menu on the right (unpair, bridge info).
- **Device chips row**: `All` plus one chip per machine. For now that's just this Mac: a green dot when connected, a laptop/desktop SF Symbol, and the Mac's name.
  - Bridge: add `GET /machine` → `{"name": "Aman's MacBook Pro", "kind": "laptop|desktop", "os": "macOS 26.x"}` (from `scutil --get ComputerName` / `sysctl hw.model`).
  - Treat it as a list so multiple machines can plug in later.
- **Projects** section: one row per folder (herdr workspace). Each row has a folder icon, the name and a chevron, and a compose icon on the right that opens a new chat in that folder.
  - **Collapsed by initially.** Tapping a row expands it inline, showing that folder's chats with an animated chevron rotation. Expansion state is remembered per folder in UserDefaults.
  - The folder row shows a small badge when any of its chats needs input.
- Chat rows follow `filter-menu.png`:
  - A leading status glyph: spinner = working, orange hand = needs input, eye/dot = ready for review (done and unseen), hollow circle = idle, check = completed.
  - Title on the first line. Second line: "Waiting for you · 10m", or "folder · 10m" in flat lists.
- Search stays, but as a glass search button/field that doesn't take a full row at the top. It could go bottom-left as in `filter-menu.png`, next to a "New chat" capsule. Pick what looks best and justify it in the report.

## 4. Filter menu (iOS)
- The top-left sidebar button (the lines icon) opens a glass menu, exactly like `filter-menu.png`:
  - **All**
  - **Needs input** (blocked)
  - **Ready for review** (done and not yet opened)
  - **Working**
  - **Completed** (done and seen, or idle)
  - **Archived**
- When the filter isn't All, the Projects tree is replaced by a flat **Sessions** list of matching chats (newest first), with the folder name in the subtitle.
- An orange dot on the filter button when anything needs input.
- **Archive**:
  - Swipe-to-archive and a context-menu Archive/Unarchive action on chat rows.
  - It's client-side only: store archived agent ids in the App Group / defaults. It doesn't close the agent.
  - Archived chats are hidden everywhere except the Archived filter.
  - An archived agent that becomes blocked un-archives itself, so you never miss a question.
- The filter is remembered across launches.

## Process
- herd-bridge: the bridge parts of 2 and 3, api.md first. Tell herd-ios when each is live on 7878.
- herd-ios: everything else. Mock first, then live.
- Screenshots go to `docs/screenshots/round2-*.png`, dark and light for the sidebar.
- Tests: unit + mock UI + live UI (w14:p2).
- Commit only your own paths. Reports: add a "Round 2" section.

## Testing on the physical device (from the next change onward)
The user wants UI tests run on their iPhone, not the simulator.
- Destination: `-destination 'id=00008110-000414D91422801E'` (Aman's iPhone 13, iOS 26.5). Signing: `DEVELOPMENT_TEAM=TC56945264 CODE_SIGN_ENTITLEMENTS=Herd/Herd-FreeTeam.entitlements -allowProvisioningUpdates`. That's a free team, so no App Groups and a limited number of new App IDs per week. Don't create new bundle ids beyond the existing app and UI test runner.
- The phone can't reach 127.0.0.1 on the Mac. Live tests must pair with the Tailscale URL: `herd://pair?url=http%3A%2F%2F100.101.102.103%3A7878&token=<token>`. That's the same bridge the user already pairs with, so their pairing stays valid.
- **The phone holds the user's real app state.** Tests must not wipe it. `-resetSidebar` and any other test-only state must write to a separate defaults domain or keys when running under UI tests, or snapshot and restore the user's values. Never touch their archived list, filter, expanded projects or seen state. Leave the app paired to the Tailscale bridge afterwards.
- The phone must be unlocked and awake. If `xcodebuild` reports the device as locked or unavailable, stop and ask herd's lead (the user) rather than retrying in a loop.
- Keep the simulator for fast iteration if useful, but the final verification run for each change is on the device, and the report says so.
- After testing, reinstall the normal app build: `TEAM=TC56945264 scripts/install-device.sh`.
