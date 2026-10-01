## Slack-driven sessions

These rules apply only when the current working directory contains a `.agents/`
directory. That means a Slack-driven session. Otherwise ignore everything below.

The person you are working with is in Slack. Per turn they see exactly one
thing: the contents of `.agents/reply.md`. They do not see your terminal, your
tool calls, or your logs.

### Every turn

1. End every turn by overwriting `.agents/reply.md` with your reply. It is the
   only thing the person in Slack sees. Never narrate tool calls, never paste
   logs.
2. Keep the reply under 12 lines. Lead with the outcome. Bullets for parallel
   items. They can ask you to elaborate — do not pre-empt that.

### Formatting is Slack mrkdwn, not GitHub markdown

- bold is `*single asterisks*`, italics `_underscores_`, strike `~tildes~`
- no `#` headings — use a bold line instead
- bullets are `•` or `-` at line start, never nested deeper than one level
- inline code in backticks, blocks in triple backticks with NO language tag
- links are `<https://url|label>`
- no tables — use a code block when columns matter

### Long content goes to an artifact

Anything longer than the reply — a plan, a design, a review, a long diff
explanation — goes in `.agents/artifacts/<kebab-name>.md`, written in normal
GitHub markdown. Reference it from the reply on its own line, exactly:

`artifact: <kebab-name>.md`

One line per artifact. The bot uploads those files to the thread. Revising a
plan means overwriting the same file; never create a v2.

### Decisions

When you need a decision before continuing, stop the turn and end the reply with
exactly this block:

```
decision:
1. <option>
2. <option>
3. <option>
```

Two to four options, one line each, first option is your recommendation. The bot
renders them as buttons, and the next message in the thread is the answer. When
a decision block fits, use it instead of asking an open question in prose.

A question you ask through the terminal UI — a permission prompt, an
AskUserQuestion-style dialog — is invisible in Slack. Use a decision block.

### Never

Never commit or push unless the thread asks for it.
