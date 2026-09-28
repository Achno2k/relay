# go-transcripts report

Package: `bridge-go/internal/transcript`. Commits: 59e97a6, df041aa, f460552, 7e19ad1.

## What changed
- Ported from Swift:
  - `PathScrubber` → `Scrubber`
  - `ToolSummary` → `Summary`, `InputString`, `Preview`, `FirstLine`, `Truncate`
  - `TranscriptParser` → `Parser`, `Parse`
  - `TranscriptLocator` → `Locator`
  - `CodexRollouts`
  - `TranscriptTailer` → `Tailer`
  - `AgentService.readBounded` → `ReadBounded`, `MaxReadBytes`. go-server calls it.
  - `CwdName`, `ProjectDirName`
- Seams:
  - `transcript.Uploads` is an interface (`LookupSent`, `ParseMarker` → `Attached`), and `*uploads.Store` satisfies it. `uploads` imports `transcript`, never the reverse.
  - `StripPastedContent` lives here.
  - `NextGrapheme`, `GraphemeCount` and `GraphemePrefix` are exported for go-live.
- Foundation/ICU behaviour reproduced in Go so output is byte-identical:
  - Swift `Character` counting for truncation (grapheme clusters: CR LF, ZWJ emoji, flags, modifiers).
  - The ICU path regex. RE2 has no lookbehind, so it's hand-rolled; `\s` is `[\t\n\f\r\p{Z}]`.
  - `JSONSerialization` output for `toolCall.input`:
    - keys in `localizedStandardCompare` order (`a` < `A` < `Z`, digits numeric, `_` < `-`);
    - `%.17g` doubles;
    - big integers kept digit for digit;
    - `-0`;
    - escapes.
  - `JSONSerialization` parsing:
    - the first duplicate key wins;
    - trailing commas and a BOM are accepted;
    - lone surrogates, invalid UTF-8 and `1e400` reject the line.
- Tailer: fsnotify (kqueue/inotify) with a 1 s safety re-check, or 250 ms polling if fsnotify can't start.
  - Dies on delete, rename or replacement (compares file identity), so the monitor recreates it.
  - Truncation re-parses from the start.
  - Keeps only the last message in memory.
  - `Stop` waits, so no callback runs after it.
  - On Linux, a delete while the file is open shows up as chmod, which the identity check catches.
- Linux: for codex's `notBefore`, "rollout created" is the `session_meta` timestamp. Linux stat has no birth time, and mtime would let a new agent borrow a busy sibling's rollout. macOS keeps the birth time, like Swift.

## Bugs fixed (docs/qa/round8.md)
- R8-8: `file:///…` and `host:/…` paths are scrubbed. `:` no longer blocks a path; URLs are safe because `://` can't match.
- R8-9, per relay-lead and api.md c28fd33:
  - `<task-notification`, `<bash-stdout>` and `<bash-stderr>` user lines are dropped;
  - `<bash-input>cmd</bash-input>` becomes `! cmd`.
  - Side effect, same as for the prefixes dropped before: the assistant turns around a dropped line merge into one message.
- R8-15: the tailer seeds from at most the last 64 MB, aligned to a newline.
- R8-16: the watch starts before the seed read.
- R8-19 (transcript part): the session-id → path caches in `Locator` and `CodexRollouts` are capped at 256.
- Found in the port: `ConsumeData` was O(n²) on one long assistant message (a copy per line). Snapshots are now taken once.

## Tests: Swift 30 → Go 45 (+2 fuzz targets)
| Swift suite | Swift | Go (same names) | Go-only additions |
|---|---|---|---|
| TranscriptParser | 11 | 11 | injectedLines (R8-9), InputString vs Foundation (escapes/numbers, key order: 400 Swift-sorted sets) |
| CodexTranscript | 5 | 5 | |
| Tailer | 2 | 2 | pollingFallback, diesOnRotation, diesWhenReplacedAtomically, truncationStartsOver, stopEndsCallbacks, missingFileIsDead, seedsFromTheTailOfAHugeFile (R8-15), writeBetweenSeedAndWatchIsNotLost (R8-16) |
| HugeTranscript | 3 | 3 | |
| PathScrubber | 3 | 3 | lookaroundEdges, fileURLsAndHostPaths (R8-8), SwiftGolden (~1k Swift outputs: scrub, truncate, count, firstLine) |
| Fuzz | 6 | 2 here | the other 4 are ported by their owners: 3 in `internal/approval` (go-live), 1 in `internal/uploads` (go-drivers), all present |
| – | | | Locator: claude dir/slow path, pi path, notBefore, caches bounded (R8-19); SwiftParserParity; FuzzScrub, FuzzParseLine |

Coverage: 89.9%.

## How it was verified
- `gofmt -l` is clean. `go vet` passes, and so does `GOOS=linux go vet`.
- `go test -race` passes on macOS.
- `go test -race` also passes on Linux (podman `golang:1.24`), tailer on inotify included.
- Native fuzzing: 20 s each, `FuzzParseLine` (1.5M execs) and `FuzzScrub` (1.3M), no crashers.
- Swift oracle: a scratch SwiftPM harness links the real `RelayCore` (nothing in `bridge/` touched).
  - Synthetic edge corpus (`testdata/parity/*.jsonl`): Go matches Swift message for message and block for block. It's committed as a test.
  - Local real transcripts (100 files: claude, codex, pi; 1,314 messages): run against the pre-fix commit, 0 diffs besides attachment markers (Swift's store vs nil).
    - With go-drivers' `uploads.Store`, all 23 attachment user messages match.
    - Nothing from that sweep is committed.

## What's left
- Wiring and live re-verify on 7880 belong to go-server and qa-bridge. qa-bridge has been told R8-8/9/15/16 are ready.
- Known and accepted gaps: collation is exact for ASCII and Latin-1 letters. Other scripts in tool-input keys sort by code point within their class, which may differ from ICU.
