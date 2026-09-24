# qa: break it and write it down

You don't change code. Your job is exploratory and scripted testing of the whole system, filing bugs other sessions can act on.
- Build and run on the simulator, and on the iPhone (id 00008110-000414D91422801E) when Xcode has an account. Use `scripts/install-device.sh`; the user may be using the phone, so ask them in the herdr lead pane before taking it over.
- Cover:
  - every flow in `docs/api.md` and the reports, for claude, codex and pi agents in `herd-e2e`;
  - pairing and re-pairing;
  - stop mid-tool;
  - approvals of every shape;
  - attachments (image, PDF, 10 files, a 20 MB file);
  - controls on each kind;
  - archive and filters;
  - the new chat sheet;
  - live typing once it lands;
  - background/foreground;
  - bridge restart (tell the others first);
  - airplane mode.
- Each bug goes to `docs/qa/bugs.md` with an id, severity (P0–P3), owner session (live-typing / bridge-harden / ios-harden / ios-polish), exact repro steps, expected vs actual, and a screenshot path. Notify the owner in herdr for P0/P1.
- Re-test fixes as owners mark them done, and keep a status column.
- At the end: `docs/qa/summary.md` with what was covered, the open bugs by severity, and a go/no-go for daily use.
