# qa-bridge report (round 8)

## What changed
- No code. Bridge bugs are filed in `docs/qa/round8.md` (Bridge section): R8-1 to R8-25, each with repro, expected result, fix and owner.
  - R8-1 to R8-21 came from my audit.
  - R8-13 and R8-25 came from go-parity; R8-22 and R8-23 from go-live.
  - R8-24 came up during re-verification.
- Severity: 3 × P1, 12 × P2, 10 × P3.
- relay-lead decided R8-9 and R8-11 in `api.md` (c28fd33).

## Tests ported
- None; this session owns no code. Swift 0 / Go 0.
- While checking fixes I ran throwaway probes from my scratchpad: a fake herdr socket, a WS listener, and extra scrubber/parser cases on a copy of the module. None were committed.

## How it was verified
- Audit:
  - Read all of `bridge/Sources/RelayCore` line by line.
  - Live probes against 7878, sending prompts, keys and controls only to the e2e agents (w14:p2 claude, w14:p4 pi); other agents got read-only GETs.
  - A throwaway Swift bridge (temp `RELAY_HOME`, port 7890) on a fake herdr socket that answers, drops or hangs. The live bridge was never restarted.
- Re-verification ran on my own Go build with a temp home, on 7891 first and again on 7880 (dcb77f1) once go-parity freed it:
  - Against real herdr and the e2e agents, on both ports: R8-1/2/3/4/5/8/9/10/11 (pi)/13/14/18. R8-24 on 7880.
  - Port 7891 against the fake herdr: R8-6/7/12/20/21. Port 7892 against the fake herdr: R8-25 (a client that never answers pings is dropped at 45 s).
  - Package tests only (not reproduced live): R8-15/16/17/19/22/23.
- Checked and not a bug:
  - WS auto-ping in Swift (on by default). The Go port lost it (R8-25, now fixed).
  - Body cap, JSON errors and key validation.
  - Upload limits, file name sanitising and attachment id lookup.
- Not filed because it didn't reproduce: after an approval, text that had already landed would reappear as live text. It doesn't happen, because Claude writes that text only after the approval.

## What's left
- R8-11 on codex: not run live, to save codex quota. The code path is the same as pi's, and go-server tested codex's input box by typing into it.
- R8-17: not run live, because the test would delete `~/.claude/settings.json`.
- Nothing else open on the bridge side.
