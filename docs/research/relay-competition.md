# Relay: competition and monetization

Researched 2026-09-29. All sources opened on that date. Labels: **[F]** fact from the linked page, **[E]** third-party estimate or claim, **[I]** my inference.

## Answer

Keep Relay personal, or open-source it. Don't build a business on it. The big labs now ship the core feature for free inside plans users already pay for. Claude Code Remote Control is GA on Pro and up. Codex works in the ChatGPT app. Copilot CLI remote control is GA [F]. The multi-agent niche Relay would target is also taken, by free open-source apps with far more traction. Happy has 24k stars and 1,017 App Store ratings. Orca has 81k stars and already runs Claude Code, Codex, OpenCode and **pi** with a mobile companion across hosts [F]. Omnara, the one funded startup that charged for this, now pitches itself as a managed-agents platform and gives the remote app away [F]. There is paid demand, but it is small. Moshi, a terminal app aimed at agents, sells a $6.99–7.99/month tier [F]. Relay's one real edge is herdr-native control over Tailscale with no hosted relay. Even there, herdr-remote and herdrm already exist [F].

## Evidence table

| Product | Owner | Agents | Own machine? | Native iOS? | Price | Traction | Source (date) |
|---|---|---|---|---|---|---|---|
| Claude Code Remote Control | Anthropic | Claude Code | Yes. CLI, Desktop, VS Code; server mode up to 32 sessions | Yes (Claude app) | Bundled, Pro/Max/Team/Ent; no API keys, no Bedrock | GA, no preview label; top RC issues have 80–165 reactions | [docs](https://code.claude.com/docs/en/remote-control) (live 2026-09) |
| Codex in ChatGPT mobile | OpenAI | Codex | Yes. Desktop app host (Mac/Win) plus SSH hosts; `codex remote-control start` daemon on Linux exists but is buggy | Yes (ChatGPT app) | Bundled; plan tiers not stated on pages opened (a blog claims all plans incl. free [E]) | Launched 2026-05-14 | [learn.chatgpt.com](https://learn.chatgpt.com/docs/remote-connections); [9to5Mac](https://9to5mac.com/2026/05/14/openai-brings-codex-control-to-chatgpt-for-iphone-and-android/) (2026-05-14); [#41638](https://github.com/openai/codex/issues/41638) (2026-08-30) |
| Copilot CLI remote control | GitHub | Copilot CLI | Yes | Yes (GitHub Mobile) | Bundled; Business/Ent need admin | GA 2026-05-18 | [changelog](https://github.blog/changelog/2026-05-18-remote-control-for-copilot-cli-sessions-now-generally-available-on-mobile-web-and-vs-code/) |
| Copilot coding agent | GitHub | Copilot (cloud) | No, Actions VMs | Yes | Copilot plans | Mobile assign 2026-04-01 | [changelog](https://github.blog/changelog/2026-04-01-github-mobile-faster-more-flexible-agent-assignment-from-issues/) |
| Cursor cloud agents | Cursor | Cursor | No, cloud VMs | No, PWA | Paid plan plus API-rate usage | n/a | [eesel](https://www.eesel.ai/blog/cursor-ios-app) (2026-06-17) [E] |
| Jules | Google | Gemini | No, cloud VMs | No mobile app found | 3 tiers (15/100/300 tasks per day) | GA at I/O 2026 [E] | [jules.google](https://jules.google/); Gemini CLI remote request closed as dup, [#22559](https://github.com/google-gemini/gemini-cli/issues/22559) |
| Happy | Slopus / Bulka LLC | Claude Code, Codex | Yes | Expo (RN), plus web and Android | Free, MIT; "Plus" $19.99/mo IAP (voice) | 23,944 stars; 4.87★ from 1,017 ratings | [GitHub](https://github.com/slopus/happy); [App Store](https://apps.apple.com/us/app/id6748571505) |
| Orca | Stably AI | Claude Code, Codex, OpenCode, pi | Yes, multiple hosts and SSH | Yes (App Store) | Free, MIT | 81,155 stars; 4.61★ from 31 ratings | [GitHub](https://github.com/stablyai/orca); [mobile docs](https://www.onorca.dev/docs/mobile) |
| Omnara | Omnara Inc (YC S25) | Claude Code, Codex, API models | Yes, plus hosted sandboxes | Yes | Free platform; pays for managed machines | $500K raised [E]; 2,877 stars; 4.44★ from 36 ratings | [pricing](https://www.omnara.com/pricing); [Launch HN](https://news.ycombinator.com/item?id=46991591) (2026-02-12) |
| CloudCLI (claudecodeui) | siteboon | Claude Code, Cursor CLI, Codex | Yes, or hosted | No, web | Free AGPL; cloud from €7/mo | 13,849 stars | [GitHub](https://github.com/siteboon/claudecodeui) |
| VibeTunnel | amantus-ai | Any terminal | Yes, Mac | iOS app "not recommended" | Free, MIT, donations | 4,675 stars; last push 2026-08-05 | [GitHub](https://github.com/amantus-ai/vibetunnel) |
| Moshi | Moshi Tech | Any terminal (mosh/tmux/herdr) | Yes | Yes | Free; Pro $6.99–7.99/mo, $59.99–69.99/yr, $249 lifetime | 4.74★ from 546 ratings; #54 Developer Tools | [App Store](https://apps.apple.com/us/app/id6757859949) |
| herdr-remote / Herdi | dcolinmorgan | herdr agents | Yes, local or own relay | No (web and menu bar) | Free | 394 stars (created 2026-06) | [GitHub](https://github.com/dcolinmorgan/herdr-remote) |
| herdrm | missuo | herdr agents, multi-host over SSH/Tailscale | Yes | macOS only | Free | 721 stars (created 2026-08) | [GitHub](https://github.com/missuo/herdrm) |
| Cosyra | Cosyra | Claude Code, Codex, OpenCode, Gemini (in cloud container) | No | Yes | $29.99/mo | n/a | [cosyra.com](https://cosyra.com/guides/cosyra-vs-happy-coder.html) [E] |

There is a long tail of small Show HNs with 1–9 points each: Chatcode, Anyware, Claude Remote, Relayd, Vicoa, Kirikiri and Onepilot ([HN search](https://hn.algolia.com/?q=remote+control+claude+code)). herdr itself has 41,352 stars [F].

## Demand signals

Reddit and X were not reachable. Reddit returns 403 and a login wall to my fetchers. The signals below come from HN and GitHub issues.

- **People want it.** "Support headless remote Linux hosts for Codex mobile" has 63 reactions and 22 comments ([#23200](https://github.com/openai/codex/issues/23200), 2026-05-17). One commenter said on 2026-09-03 that this gap had kept them on Claude Code. Also "Make codex-cli sessions available in mobile app" ([#38963](https://github.com/openai/codex/issues/38963), 2026-08-17) [F].
- **Official apps are flaky.** Claude RC "automatic reconnection doesn't work" has 108 reactions and 72 comments and is still open ([#34255](https://github.com/anthropics/claude-code/issues/34255)). "Sessions die after ~20 min idle" has 86 ([#32982](https://github.com/anthropics/claude-code/issues/32982)). "Codex mobile shows running desktop as offline" has 42 ([#22898](https://github.com/openai/codex/issues/22898)) [F].
- **People won't pay much.** On Omnara's Launch HN, one user wrote "At $9 I'd be totally in, but moving from CC's Max plan at $100, adding $20 makes me wanna just hack an alternative." Another said "anyone can build this for themselves so easily." A Happy user runs "the Happy server on my home Mac to skip the cloud relay" and tries "to only pay for the model" ([HN](https://news.ycombinator.com/item?id=46991591), 2026-02-12) [F].
- **Privacy is a blocker.** "If you can see the messages unfortunately that's a deal breaker" (Omnara thread). "I don't see our company agreeing to allow some third party potential access to our chats" ([Chatcode HN](https://news.ycombinator.com/item?id=48396686), 2026-06-04) [F].
- **Reliability decides who wins.** "Happy Coder ... I found it rarely works reliably" ([Ask HN](https://news.ycombinator.com/item?id=46742298), 2026-01-24). Happy's App Store reviews complain about slow long chats and unresponsive approval buttons [F].
- **Voice** is the most requested missing feature per one Aug 2026 guide ([explainx](https://www.explainx.ai/blog/claude-code-mobile-remote-control-phone-guide-2026)) [E]. Happy, Omnara and Moshi ship it [F].

## Gap analysis

| Relay angle | Real? | Wanted? | Already covered by |
|---|---|---|---|
| Several agent brands in one app | Yes | Yes | Orca (incl. pi), Happy, Omnara, CloudCLI, all free [F] |
| Multi-machine incl. Linux VMs | Yes | Yes, Codex #23200 | Claude RC (any machine that runs `claude`), Orca multi-host, herdrm; Codex Linux path is semi-official and buggy [F] |
| Native SwiftUI quality | Partly | Reliability complaints say yes | Official Claude and ChatGPT apps are native; Orca has an App Store app; Happy is Expo [F]. The edge is real but hard to show in a listing [I] |
| No hosted relay (Tailscale) | Yes | Niche but loud (HN privacy comments) | Happy self-hosted server, VibeTunnel, herdr-remote, Moshi. Anthropic and OpenAI both route through their own servers [F] |
| Usage across subscriptions | Partly | Yes on desktop | CodexBar (22,044 stars, 87 providers, macOS/Linux) and ccusage (18,794 stars) [F]. No iOS equivalent found [I] |
| herdr-native (status from herdr, not guessed) | Yes | Unknown | herdr-remote (web/Telegram), herdrm (macOS only). No native iOS herdr client found [I] |

Every angle except "native iOS herdr client" and "phone-side cross-subscription usage" is already covered by a free product. Those two are features. Neither is a business on its own [I].

## Pricing benchmarks (iOS dev tools, US App Store, 2026-09-29)

| App | Model | Price |
|---|---|---|
| Moshi (agent terminal) | Subscription or lifetime | $6.99–7.99/mo; $59.99–69.99/yr; $249 lifetime; founder tiers $3.99/mo, $19.99/yr |
| Termius | Subscription | $15/mo, $119/yr |
| Blink Shell | Subscription | Blink+ $19.99; Build from $7.99/mo |
| Prompt 3 (Panic) | Both | $9.99/yr or $49.99 one-time |
| Textastic | Both | $2.99/mo, $19.99/yr, $69.99 one-time |
| Working Copy | One-time | $35.99 |
| Secure ShellFish | One-time | $29.99 |
| Happy | Free plus IAP | Plus $19.99/mo |

The usual range is a one-time unlock of $30–70 or $20–70/yr. Agent-specific apps charge more per month (Moshi, Happy). HN users push back on anything near $20/mo stacked on a $100–200 model plan [F]/[I].

## Recommendation

**Option A: keep it personal (the default).** No support cost, and you can still break the API every round.

**Option B: open-source it (recommended if you want reach).** Put the bridge and the app under MIT and ship on TestFlight or the App Store for free. Pitch it as "the native iOS client for herdr". herdr has 41k stars, and nobody owns that slot on iOS yet [F]/[I]. Upside: reputation and contributors, and herdr might link to it. Risks:
- Orca or the herdr team could ship the same thing. Orca already supports pi and multi-host [F].
- The App Store needs a working demo for review. The existing mock backend covers this.
- Issues arrive from setups you can't reproduce (Linux distros, Tailscale ACLs).
- The rules against real transcripts and paths in fixtures matter more once the repo is public.

**Option C: paid app (not recommended now).** A realistic ceiling is Moshi-like, $5–8/mo or about $50/yr with a lifetime tier [I]. Blockers:
1. Free, better-known rivals exist for every feature [F].
2. Anthropic and OpenAI improve their own clients monthly and set the baseline at $0 [F].
3. Relay depends on herdr plus Tailscale, so the buyer pool is herdr users on iPhone who don't want a hosted relay. I can't size that from public data [I].
4. Relay parses the Claude, Codex and pi transcript formats and herdr's socket API. Paid users would expect fixes the same day those formats change [I].
5. A paid app that drives other vendors' agents invites store review and trademark questions ("Claude", "Codex" in the listing) [I].

If you still want to test paid demand, go open core. Ship Option B, add a one-time "supporter" unlock ($20–30) for extras like the cross-subscription usage widgets or Live Activities, and watch conversion for 60 days before going further [I].

## Gaps in this research

- Reddit and X were not accessible, so the demand signals rely on HN and GitHub.
- There is no download or revenue data for any app. Ratings counts are only a proxy.
- The Cursor and Jules rows rest on third-party pages. cursor.com was not opened directly.
- The Codex Linux CLI remote-control path appears only in issues. I found no official doc for pairing it.
