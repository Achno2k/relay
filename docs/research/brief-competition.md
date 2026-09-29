# Research brief: is there a business in Relay?

Relay: a native iOS app (SwiftUI, iOS 26) for watching and steering CLI coding agents that run on the user's own machines. Supported agents: Claude Code, Codex CLI, pi.
- Works through a small bridge daemon per machine, which talks to herdr (a terminal multiplexer for coding agents), over Tailscale.
- Features:
  - chats rebuilt from agent transcripts, with live typing;
  - approvals and questions answered from the phone;
  - model / effort / mode controls;
  - attachments;
  - usage across subscriptions (Claude, ChatGPT, OpenCode);
  - multi-machine (Mac plus Linux VMs).

Question: is there a real monetization opportunity, or is this best kept as a personal tool?

Find, with sources (open the pages; snippets aren't evidence):
1. **Official offerings** that let you monitor or steer coding agents from a phone or the web:
   - Anthropic: Claude Code on the web / mobile, remote control, sessions;
   - OpenAI: Codex cloud / mobile;
   - Cursor: background agents / mobile;
   - GitHub Copilot coding agent on mobile;
   - Google.

   For each: what it does, whether it works with agents on *your own machine*, and pricing (bundled or extra).
2. **Third-party tools** in the same space, e.g. Happy / happy.engineering, Omnara, VibeTunnel, Claude Code UI / claudecodeui, and anything newer. For each:
   - pricing and model (open source, free, paid);
   - which agents it supports;
   - native or web;
   - self-hosted or a hosted relay;
   - traction: GitHub stars, App Store ratings, funding, Product Hunt, HN threads.
3. **Demand signals** from Reddit (r/ClaudeAI, r/ChatGPTCoding, r/codex etc.), HN and X: what people want from mobile agent control, their complaints, and what they'd pay for.
4. **Gaps** Relay could own: multiple agent brands, multi-machine, native quality, no hosted relay (privacy), usage across subscriptions. Are they real and wanted, or already covered?
5. **Pricing benchmarks** for comparable indie developer tools on iOS (one-time vs subscription, typical prices).

Output: write `docs/research/relay-competition.md`, containing:
- a one-paragraph answer first;
- an evidence table (product, owner, agents, own-machine?, native?, price, traction, source URL + date);
- demand signals with links;
- a gap analysis;
- a recommendation with options (personal / open source / paid), including risks.

Keep it under about 1,500 words. Mark facts vs estimates vs inference. Today is 2026-09-29, so prefer 2026 sources and date everything. Don't invent numbers.
