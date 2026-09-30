# Round 10: r10-bridge report

Branch `r10/bridge`. Go bridge only (`bridge-go/`); the Swift `bridge/` is untouched.

## Commits
| sha | what |
|---|---|
| 77c620f | api.md contract: `GET /agents/:id/file` + `FileContent`, toolCall `path` / `edit` (`ToolEdit`) / `plan`, `Approval.plan` |
| 787d0b7 | fixtures `file.json`, `messages-edits.json`, `approval-plan.json`; Go api types |
| 1b1c546 | B2: `GET /agents/:id/file` (`internal/files`) |
| dad1086 | B2 + B7: toolCall `path`/`edit`, ExitPlanMode `plan`, `Approval.plan`; parity test fixes |
| d375606 | codex title animation kept out of `agent.title` |

## B2 file viewer (bridge side)
- `GET /agents/:id/file?path=` reads a cwd-relative file.
  - The path is realpath'd (symlinks followed) and must stay inside the realpath'd cwd, else `403 forbidden`.
  - Lexical `..` and a dangling symlink that points outside are `403` too, so the answer doesn't reveal whether an outside file exists.
- Text is JSON `FileContent`, capped at 1 MB and cut on a newline. Images whose magic bytes match their extension come back as raw bytes. Other binaries get `415`.
- toolCall `path` is set only when the file is inside the cwd.
  - It's decided before scrubbing: the scrubber keeps an outside path's last component, which would otherwise look relative.
  - Claude's plan file and uploads (outside the cwd) have no `path`.
- toolCall `edit`: Claude Edit/MultiEdit/Write, pi edit (`edits[]` or top-level `oldText`/`newText`)/write, codex FileChange (unified diff with `---/+++` headers added; a single added file is `write`). Strings are scrubbed. Caps: 64 KB per string, 256 KB per edit.

## B7 plan
- ExitPlanMode's `toolCall.plan`: `input.plan`, else the plan file on disk (the session's last Write/Edit under `~/.claude/plans/`), else that Write's content. It's scrubbed and capped at 256 KB.
- `Approval.plan` (Claude only):
  - the plan of the unanswered ExitPlanMode at the transcript tail;
  - else, when the question is the plan prompt ("…plan… ready to execute/proceed"), the session's latest plan file.
- Checked on w14:p2 history: the ExitPlanMode row carries the plan.

## B6 root cause
- The app's Stop button and greyed composer come only from `agent.status == .working` (`ChatView` → `ComposerView.showsStop`).
- herdr and the Go bridge were clean on the e2e codex (ask mode) and pi, across these cases:
  - a plain turn;
  - a tool turn;
  - a prompt sent mid-turn;
  - a curl approval answered through `/approval` + `/keys`.

  herdr flips to `done` ≤1.5 s after the screen goes idle, and the bridge sends `agent.updated done` + `reply.live null` at once.
- The screenshot came from the live **Swift** bridge (7878, built 09-25). Its monitor logic is the same as Go's.
- Cause: an app race.
  - `AppStore.refresh(c)` runs on foreground `resume()`, on every WS reconnect and at start. It fetches `/machine`, `/health`, `/agents` and `/workspaces` in parallel, then `merge()` replaces the agents.
  - An `agent.updated(done)` that arrives after the bridge built the `/agents` snapshot but before `merge()` gets overwritten with `working`.
  - The bridge never resends (nothing changed), so Stop sticks until the next status change.
- Fixed by r10-chat in `r10/chat` 25b2fcb: `merge` keeps agents the socket touched after the refresh started.

## Other findings
- Pre-existing: `TestSwiftParserParity` failed on master a day after the Swift capture: its "no timestamp → now" check had a 24 h window. Fixed; round-10 fields are ignored there too, since the Swift parser doesn't have them.
- codex animates its terminal title. The spinner, and a blinking `[ ! ]`/`[ . ] Action Required |` prefix while blocked, reached `agent.title` and sent an `agent.updated` per frame. Now stripped.
- codex live parser, not fixed:
  - Prose bullets that start with a tool verb ("• Running the requested command.") parse as a Shell tool.
  - Once, a final "• done" came out as `tool {name: Tool, summary: done}`; I couldn't reproduce it after a sender collision.

  Low impact: B3's shimmer would say "Running…" for a moment.
- pi never sent a live `tool` frame. That's by design: the tracker drops a live tool once its `toolCall` is in the transcript, and pi writes the call when the tool starts. api.md "Live reply" now says so. r10-chat's shimmer already uses the last unanswered `toolCall` while working (r10/chat 46d5ebd).

## Open
- `Approval.plan` on a live plan prompt: the history side is checked on w14:p2, and r10-qa is checking the prompt itself on :7883.
- Test bridge: 127.0.0.1:7883, `RELAY_HOME` in my scratchpad. I'll stop it at the end.
