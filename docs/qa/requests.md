# Cross-session requests

## usage-pi → ios-harden (2026-09-25) — DONE

Lead (herd-native) confirmed ios-harden is finished for this round and cleared usage-pi to make this
edit directly. Done: `UsageProvider.usedBy: [String]` added in
`ios/RelayKit/Sources/RelayKit/Models.swift`, default `[]`, decoded with `decodeIfPresent` so older
bridge payloads without the field still decode.

Original request, kept for context:

Need a small addition to `ios/RelayKit/Sources/RelayKit/Models.swift` (your path), for the usage-pi
round-5 task (per-subscription `usedBy` on the Usage screen, go-ahead from herd-native):

- `UsageProvider` (around line 500) needs a new field: `public var usedBy: [String] = []` (Codable,
  Hashable, Sendable — default `[]` so old cached/decoded payloads without the field still decode).
  Add it to the memberwise `init` too, with a default of `[]`.

This mirrors the bridge-side `UsageProvider.usedBy: [String]` I'm adding in
`bridge/Sources/RelayCore/UsageModels.swift`. Nothing else in `Models.swift` needs to change — window
shape, `UsageSnapshot`, `ServerEvent.usageUpdated` all stay as-is.

If you'd rather I do it myself since it's a one-field additive change, say so and I will — flagging here
first since `ios/RelayKit/` isn't my path per round-5 README. Not blocking my bridge-side work or the new
`opencode-go` provider entry; it does block wiring `usedBy` through into `UsageView.swift`'s "Used by"
line, so I'll stub around it (read via a local decode-tolerant shim if needed) until this lands.
