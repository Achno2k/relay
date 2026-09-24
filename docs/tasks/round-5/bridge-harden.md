# bridge-harden: make the bridge boring and unbreakable

Audit and fix. Write a test for each fix.
- **herdr socket:**
  - herdr restarts, the socket disappears or reappears, slow replies, partial lines. The event subscription must reconnect, and REST must answer `503 herdr_unavailable` fast, never hang.
  - Kill and restart herdr's server during a test to prove it. Coordinate: the user's agents run in herdr, so use `herdr server reload-config`, or simulate with the fake socket rather than stopping their server. **Never stop the user's herdr.**
- **Transcript tailing:** file rotation, truncation, huge files (>50 MB), invalid UTF-8, partial JSON lines, sessions switching (`/clear`, `/new`), and deleted cwd (the /tmp wipe we hit). No crash, no unbounded memory.
- **Input validation:** body size limits on every route, ids and keys validated against herdr's allowed names, and attachment names sanitised (path traversal, unicode tricks). Fuzz the approval and screen parsers with random input.
- **Security:**
  - The token is compared in constant time.
  - Tailscale + localhost bind only; verify nothing listens on 0.0.0.0.
  - Uploads are 0600 and served only to the right agent id.
  - Grep every JSON response in tests for `/Users/` and `/private/` (no path leaks).
- **Operations:**
  - Log rotation for `~/.relay/relay.log` (size-capped).
  - Graceful shutdown on SIGTERM.
  - `/health` reports the herdr connection state and the uptime.
  - Memory and CPU are stable over a 30-minute soak with the app connected. Measure and report.
- **Consistency:** audit the error codes against api.md and fix any drift.
