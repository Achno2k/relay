# Relay

Personal iOS app that drives the coding agents in a herdr session from the phone.

- `bridge/`: Swift package (Hummingbird 2). The `relay` executable (`RelayCore` library) runs on the Mac as the LaunchAgent `com.relay.bridge`, keeps its data in `~/.relay`, talks to herdr's unix socket (`$HERDR_SOCKET_PATH`, default `~/.config/herdr/herdr.sock`) and tails agent transcripts.
- `ios/`: the Relay app. SwiftUI, iOS 26+, Swift 6, Liquid Glass, modelled on the ChatGPT iOS app. XcodeGen (`ios/project.yml` → `Relay.xcodeproj`, scheme `Relay`), shared code in the local `RelayKit` package. It keeps the bundle id `dev.amansingh.herd` and App Group `group.dev.amansingh.herd` so installs update in place. It pairs via `relay://` and still accepts old `herd://` links.
- `docs/api.md` is the contract between them. Change it first. Fixtures live in `docs/fixtures/`.
- `docs/herdr-schema.json` is herdr's socket API schema (protocol 22).

Rules
- Never commit real transcripts, agent lists or paths from this machine. Fixtures are synthetic.
- Full filesystem paths never cross the wire (`cwdName` only; tool summaries are cwd-relative).
- Status is re-derived from herdr, never cached as truth.
- No third-party UI libraries in the app.
