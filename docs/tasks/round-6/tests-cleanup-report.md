# tests-cleanup report (round 6)

## 1. Flaky bridge suite

Baseline on master (`2bd7d8f`): 27 of 30 full `swift test` runs passed. All 3 failures were the test process dying of SIGPIPE (signal 13). The brief's other symptoms ("Bad file descriptor", "herdr closed the connection", empty agent list) didn't show up in those 30 runs. The new fd test below reproduces the first two on demand.

### Root cause: a double close in production code

`UnixSocket.init` (`bridge/Sources/RelayCore/UnixSocket.swift`) closed its fd when `connect` failed and then threw. By then every stored property was set, and in that case Swift runs `deinit` on a throwing class init. So `deinit → close()` closed the same fd number a second time. Between the two closes, another thread can get that number from `socket()`/`accept()`/`pipe()`, and the second close kills its socket.

- It fires on every failed connect to herdr. The restart and "herdr is gone" tests do that constantly, so any other suite's socket could be closed under it: "Bad file descriptor" on read, "herdr closed the connection", and a response that never arrives (the empty agent list).
- This is a real bridge bug, not just a test issue. On the Mac, each failed connect while herdr is down or restarting could close an unrelated fd in the bridge: a NIO connection, another herdr call, a transcript file.
- I checked the Swift behaviour with a standalone repro: `deinit` ran after `init` threw.

Fixes in `UnixSocket`:
- The init failure paths just throw and let `deinit` close the fd once.
- `close()` is safe while another thread is inside `readLine`/`writeLine`. It calls `shutdown` to wake the blocked call. The fd itself is closed only after the last in-flight syscall returns. Before this, `HerdrEventStream.stop()`/`watch()` could close the fd between the reader's fd lookup and its `read`, so the reader would then read from whatever socket reused the number. I found this second race by reading the code. The test below doesn't separate it from the first one.
- `errno` is captured before the lock is released.

### Test harness fixes (`FakeHerdr`)

The wip branch (`a78f4cc`) added claim-once closes and "wait for the thread to start". That narrowed the windows but left them open. `stop()` still closed fds that other threads were about to use. I rewrote the fd ownership instead:
- Each fd has one owner that closes it. The accept thread owns the listening socket and a wake pipe. Each serve thread owns its connection.
- `stop()` closes nothing. It writes to the wake pipe, shuts down open connections while holding the lock that serve threads close under, and waits for the accept thread to exit. It is idempotent.
- `accept` uses `poll` on the listener plus the wake pipe, so a closed listener fd never reaches `accept()`.
- A connection accepted during `stop()` is dropped under the same lock, so a stopped herdr doesn't keep serving.
- Accepted sockets set `SO_NOSIGPIPE`. This fixes the SIGPIPE crashes: a client that gives up early (timeouts, resubscribes) made FakeHerdr's `write` kill the whole test process.
- The temp dir uses the full UUID and `withIntermediateDirectories: false`, so a path collision fails loudly instead of sharing a socket.
- `socket`/`bind`/`listen`/`pipe` failures throw instead of being ignored.

No suites were made `.serialized`.

### Regression test

`FdHygieneTests` runs a watcher thread that keeps opening pipes and counts the ones closed behind its back. Meanwhile the main thread:
- does 3000 failed `UnixSocket` connects (`failedConnectClosesItsFdOnce`);
- does 300 rounds of closing a socket while another thread is blocked reading it (`closeWhileReadingNeverClosesSomeoneElsesFd`).

On master's `UnixSocket`, both fail in 3 of 3 runs: 45 to 109 stray closes, plus "write: Bad file descriptor" and "herdr closed the connection". With the fix, both pass.

### Verification

- 50 consecutive full-suite runs with the brief's command (`for i in $(seq 50); do swift test || break; done`, 214 tests in 30 suites): 50 of 50 passed, while the other round-6 sessions were building on the same machine.
- The wip branch `wip/fakeherdr-fd-race` is deleted. Its FakeHerdr change is superseded, and its other file (`docs/tasks/round-5/bridge-harden-report.md`, which wasn't on master) is committed to master so nothing was lost.

## 2. QA-2: attachment names with `/`

The code now matches api.md: `/` becomes `-` like any other disallowed character, where before it was treated as a path separator (`lastPathComponent`). `a/b.txt` becomes `a-b.txt`, and `../../etc/passwd` becomes `etc-passwd`. I kept the api.md rule instead of documenting the old behaviour. The old behaviour silently dropped part of the user's file name, and it isn't any safer.

The no-traversal guarantee holds without `lastPathComponent`:
- only `A-Z a-z 0-9 . _ -` survive;
- leading and trailing `.`/`-` are trimmed;
- the stored name is `<16-hex id>-<name>`.

Tests:
- new `sanitize` cases for `/`, `\`, `/` alone, `../..` and `x/../../y.txt`;
- `slashNamesStayInsideTheUploadFolder` saves real files for traversal-style names and checks each one lands directly in the agent's upload folder;
- the existing 1000-name fuzz still passes.

`docs/qa/bugs.md` marks QA-2 fixed.

Live check, after live-tool-bridge rebuilt and restarted the bridge with these commits: QA-2's curl repro against `w14:p2` returned `a-b.txt`, and `../../../../tmp/relay_traversal_marker.txt` returned `tmp-relay_traversal_marker.txt` with nothing written to `/tmp`. I deleted both test uploads afterwards.

## Left open

- `SocketResilienceTests` and `RoutesTests` are still `.serialized`. Their comments blame fd races that this fix should remove. I left them as they are, since un-serialising them is a separate change with its own risk.
