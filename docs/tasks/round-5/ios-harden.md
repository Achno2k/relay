# ios-harden: state, networking and lifecycle robustness

Audit and fix. Write a test for each fix.
- **Connection:**
  - The WS drops, the bridge restarts (kickstart), Tailscale goes off and on, and the network changes Wi-Fi ↔ cellular. The app must recover by itself with backoff, show the banner honestly, and resync without duplicate or missing messages.
  - Stale requests from a previous connection must not overwrite newer state.
- **Lifecycle:** background for 10+ minutes, then foreground → the open chat and sidebar are correct within about 1 s. Also cold launch with a stale selected agent (closed pane), and memory warnings.
- **Races:**
  - A rapid agent switch while messages load.
  - An approval arriving while a control change is in flight.
  - Sending while reconnecting (queue or fail clearly; never silently drop).
  - The pending-bubble lifecycle.
  - Double taps on send, answer or control.
- **Large data:** a 5,000-message transcript and 50 agents. Scrolling stays smooth, memory stays bounded (paging, no full re-decode per event), and there are no main-thread stalls longer than 50 ms. Profile it with Instruments or os_signpost and report the numbers.
- **Errors:** every bridge error code maps to a clear user message, with no raw JSON or Swift error text in the UI.
- **Pairing edge cases:** token rotated on the Mac, bridge moved to a new port, an invalid QR code.
Coordinate UI copy with ios-polish; you own state and behaviour, it owns how things look.
