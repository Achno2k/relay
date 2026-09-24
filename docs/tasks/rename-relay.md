# Rename: Herd → Relay

The user renamed the product to **Relay**. Rename everything user-facing and the code names. **Keep these unchanged:**
- iOS bundle id `dev.amansingh.herd`, the UI-test runner bundle id, and App Group `group.dev.amansingh.herd`. This way the app updates in place on the phone, keeps its pairing and archive, and uses no new free-team App ID.
- Keychain service/account keys that store the current pairing. Either keep them, or read the old key and migrate once.
- The repo **folder** stays `~/projects/herd` for now. The lead moves it to `~/projects/relay` after you're both done. Don't hard-code either absolute path anywhere.

## Bridge (herd-bridge)
- Executable `herd` → `relay` (`relay serve|pair|token|install-launchd`). Package/product/module names: `HerdCore` → `RelayCore`, and any `Herd*` types → `Relay*`.
- Data dir `~/.herd` → `~/.relay`. On start, if `~/.relay` doesn't exist and `~/.herd` does, move it over: token, uploads, logs, and the `e2e` folder. Keep the same token so the phone stays paired. Log the migration.
- LaunchAgent label `com.herd.bridge` → `com.relay.bridge`, log at `~/.relay/relay.log`.
  - `install-launchd` writes the new plist. Don't load or unload anything yourself: the lead swaps the service.
- Pairing links `herd://pair?...` → `relay://pair?...`.
- `/health` may report `"name":"relay"`. Update api.md (title, examples, scheme) and the fixtures only where the name appears.
- `swift test` stays clean with no warnings. Commit only `bridge/` and `docs/api.md`.

## iOS (herd-ios)
- Display name, home-screen label, large sidebar title, pairing screen copy, and alerts: all say **Relay**.
- URL scheme: register `relay`, and keep `herd` as an extra scheme so old QR codes and links still pair. Parse both.
- XcodeGen: project `Herd.xcodeproj` → `Relay.xcodeproj`, scheme/app target `Herd` → `Relay`, `HerdTests` → `RelayTests`, `HerdUITests` → `RelayUITests`, `HerdKit` → `RelayKit` (package + module), and `Herd*` types → `Relay*`.
  - Keep `PRODUCT_BUNDLE_IDENTIFIER`s as they are.
  - Delete the old generated project so there aren't two.
- Test env vars `HERD_E2E_*` → `RELAY_E2E_*`.
- `scripts/install-device.sh`: scheme/project names, and the `Herd.app` → `Relay.app` path.
- App icon: leave it unless it contains the word Herd.
- Run unit + mock UI tests on the simulator plus one live UI test against the bridge. The bridge is still the old launchd service on 7878 until the lead swaps it, so pairing uses the `herd://` or `relay://` link with the same token.
- Commit only `ios/`, `scripts/`, and `docs/screenshots` if needed.

## Both
- Update `AGENTS.md` and the README-style docs you own for the new names. Leave old task briefs and reports as they are; they're history.
- When done, reply with exactly what the lead must run to swap the launchd service and move the folder.
