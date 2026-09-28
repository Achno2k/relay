package transcript

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"relay/internal/api"
)

type Format string

const (
	FormatClaude Format = "claude"
	FormatPi     Format = "pi"
	FormatCodex  Format = "codex"
)

// User lines Claude Code injects that the user never typed (api.md transcript rules).
var droppedUserPrefixes = []string{
	"<command-", "<local-command", "<system-reminder",
	"<task-notification", "<bash-stdout>", "<bash-stderr>",
}

// shellInput is the command of a shell-mode (`!`) line, `<bash-input>cmd</bash-input>`.
func shellInput(trimmed string) (string, bool) {
	rest, ok := strings.CutPrefix(trimmed, "<bash-input>")
	if !ok {
		return "", false
	}
	cmd, _, _ := strings.Cut(rest, "</bash-input>")
	return cmd, true
}

// Parser turns agent JSONL into messages, one line at a time.
//
// Pure: feed it lines, read Messages. Consume returns the message that was created or grew,
// so a tailer can emit `message.upserted` for it.
type Parser struct {
	format   Format
	scrubber Scrubber
	uploads  Uploads
	messages []api.Message
	// Time of the line being parsed, to match sent records. Zero = unknown.
	currentDate time.Time
}

// NewParser: uploads may be nil (no attachment matching).
func NewParser(format Format, cwd string, uploads Uploads) *Parser {
	return &Parser{format: format, scrubber: NewScrubber(cwd), uploads: uploads}
}

// Parse parses a whole transcript.
func Parse(data []byte, format Format, cwd string, uploads Uploads) []api.Message {
	p := NewParser(format, cwd, uploads)
	p.ConsumeData(data)
	return p.Messages()
}

func (p *Parser) Format() Format     { return p.format }
func (p *Parser) Scrubber() Scrubber { return p.scrubber }

// Messages returns the messages so far. The slice must not be modified.
func (p *Parser) Messages() []api.Message { return p.messages }

// DropSettled forgets every message but the last, which is the only one a later line can grow.
// A tailer uses it to keep memory flat on a long session.
func (p *Parser) DropSettled() {
	if n := len(p.messages); n > 1 {
		p.messages = append([]api.Message(nil), p.messages[n-1])
	}
}

// ConsumeData feeds every line in data. Returns the messages that changed, in order.
func (p *Parser) ConsumeData(data []byte) []api.Message {
	var order []string
	latest := map[string]int{} // id → index in p.messages
	for _, line := range bytes.Split(data, []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		i := p.consume(line)
		if i < 0 {
			continue
		}
		id := p.messages[i].ID
		if _, seen := latest[id]; !seen {
			order = append(order, id)
		}
		latest[id] = i
	}
	changed := make([]api.Message, len(order))
	for k, id := range order {
		changed[k] = copyMessage(p.messages[latest[id]])
	}
	return changed
}

// ConsumeLine feeds one JSONL line. Returns the message it created or grew, if any.
func (p *Parser) ConsumeLine(line []byte) (api.Message, bool) {
	i := p.consume(line)
	if i < 0 {
		return api.Message{}, false
	}
	return copyMessage(p.messages[i]), true
}

// consume feeds one line and returns the index of the message it created or grew, or -1.
func (p *Parser) consume(line []byte) int {
	o, ok := readJSONObject(line)
	if !ok {
		return -1
	}
	var m *api.Message
	switch p.format {
	case FormatClaude:
		m = p.consumeClaude(o)
	case FormatPi:
		m = p.consumePi(o)
	case FormatCodex:
		m = p.consumeCodex(o)
	}
	if m == nil {
		return -1
	}
	// Every builder returns the last message.
	return len(p.messages) - 1
}

// A snapshot the caller can keep while the parser grows the original.
func copyMessage(m api.Message) api.Message {
	m.Blocks = append([]api.Block(nil), m.Blocks...)
	return m
}

// MARK: JSON helpers, with Swift's `as?` semantics.

func obj(v any) (map[string]any, bool) {
	m, ok := v.(map[string]any)
	return m, ok
}

func str(v any) (string, bool) {
	s, ok := v.(string)
	return s, ok
}

func strOr(v any, fallback string) string {
	if s, ok := v.(string); ok {
		return s
	}
	return fallback
}

// NSNumber bridges to Bool only for 0 and 1.
func boolean(v any) (bool, bool) {
	switch v := v.(type) {
	case bool:
		return v, true
	case json.Number:
		switch v.String() {
		case "0":
			return false, true
		case "1":
			return true, true
		}
	}
	return false, false
}

func isTrue(v any) bool {
	b, ok := boolean(v)
	return ok && b
}

// `as? [[String: Any]]`: all elements must be objects, or nothing.
func objects(v any) ([]map[string]any, bool) {
	arr, ok := v.([]any)
	if !ok {
		return nil, false
	}
	out := make([]map[string]any, 0, len(arr))
	for _, e := range arr {
		m, ok := e.(map[string]any)
		if !ok {
			return nil, false
		}
		out = append(out, m)
	}
	return out, true
}

// `as? [String]`: all elements must be strings, or nothing.
func stringList(v any) ([]string, bool) {
	arr, ok := v.([]any)
	if !ok {
		return nil, false
	}
	out := make([]string, 0, len(arr))
	for _, e := range arr {
		s, ok := e.(string)
		if !ok {
			return nil, false
		}
		out = append(out, s)
	}
	return out, true
}

// `as? Int`
func integer(v any) (int64, bool) {
	switch v := v.(type) {
	case json.Number:
		if i, err := v.Int64(); err == nil {
			return i, true
		}
		if f, err := v.Float64(); err == nil && f == float64(int64(f)) {
			return int64(f), true
		}
	case bool:
		if v {
			return 1, true
		}
		return 0, true
	}
	return 0, false
}

func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%X-%X-%X-%X-%X", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func idOr(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return newUUID()
}

func createdAt(raws ...any) string {
	for _, r := range raws {
		if s, ok := api.NormalizeTimestamp(r); ok {
			return s
		}
	}
	return api.FormatTime(time.Now())
}

func lineDate(raw any) time.Time {
	if s, ok := raw.(string); ok {
		if t, ok := api.ParseTimestamp(s); ok {
			return t
		}
	}
	return time.Time{}
}

// MARK: Claude

func (p *Parser) consumeClaude(o map[string]any) *api.Message {
	typ, _ := str(o["type"])
	if typ != "user" && typ != "assistant" {
		return nil
	}
	if isTrue(o["isSidechain"]) || isTrue(o["isMeta"]) {
		return nil
	}
	// `/compact` injects the whole summary as a user message.
	if isTrue(o["isCompactSummary"]) {
		return nil
	}
	message, ok := obj(o["message"])
	if !ok {
		return nil
	}
	id := idOr(o["uuid"])
	at := createdAt(o["timestamp"])
	p.currentDate = lineDate(o["timestamp"])

	if typ == "assistant" {
		content, ok := objects(message["content"])
		if !ok {
			return nil
		}
		var blocks []api.Block
		for _, b := range content {
			if blk, ok := p.claudeAssistantBlock(b); ok {
				blocks = append(blocks, blk)
			}
		}
		return p.appendAssistant(id, at, blocks)
	}

	if text, ok := str(message["content"]); ok {
		return p.appendUser(id, at, []string{text})
	}
	content, ok := objects(message["content"])
	if !ok {
		return nil
	}
	var results []api.Block
	var texts []string
	for _, b := range content {
		switch strOr(b["type"], "") {
		case "tool_result":
			isErr, _ := boolean(b["is_error"])
			results = append(results, api.ToolResultBlock(
				strOr(b["tool_use_id"], ""), isErr, Preview(resultText(b["content"]), p.scrubber)))
		case "text":
			if t, ok := str(b["text"]); ok {
				texts = append(texts, t)
			}
		}
	}
	var changed *api.Message
	if len(results) > 0 {
		changed = p.attachResults(results)
	}
	if user := p.appendUser(id, at, texts); user != nil {
		changed = user
	}
	return changed
}

func (p *Parser) claudeAssistantBlock(b map[string]any) (api.Block, bool) {
	switch strOr(b["type"], "") {
	case "text":
		if t, ok := str(b["text"]); ok && t != "" {
			return api.TextBlock(p.scrubber.Scrub(t)), true
		}
	case "thinking":
		if t, ok := str(b["thinking"]); ok && t != "" {
			return api.ThinkingBlock(p.scrubber.Scrub(t)), true
		}
	case "tool_use":
		name := strOr(b["name"], "tool")
		input, ok := obj(b["input"])
		if !ok {
			input = map[string]any{}
		}
		return api.ToolCallBlock(strOr(b["id"], ""), name,
			Summary(name, input, p.scrubber), InputString(input, p.scrubber)), true
	}
	return api.Block{}, false
}

// MARK: pi

func (p *Parser) consumePi(o map[string]any) *api.Message {
	if strOr(o["type"], "") != "message" {
		return nil
	}
	message, ok := obj(o["message"])
	if !ok {
		return nil
	}
	id := idOr(o["id"])
	at := createdAt(o["timestamp"], message["timestamp"])
	content := message["content"]
	switch strOr(message["role"], "") {
	case "user":
		if s, ok := str(content); ok {
			return p.appendUser(id, at, []string{s})
		}
		blocks, _ := objects(content)
		var texts []string
		for _, b := range blocks {
			if strOr(b["type"], "") == "text" {
				if t, ok := str(b["text"]); ok {
					texts = append(texts, t)
				}
			}
		}
		return p.appendUser(id, at, texts)
	case "assistant":
		items, _ := objects(content)
		var blocks []api.Block
		for _, b := range items {
			switch strOr(b["type"], "") {
			case "text":
				if t, ok := str(b["text"]); ok && t != "" {
					blocks = append(blocks, api.TextBlock(p.scrubber.Scrub(t)))
				}
			case "thinking":
				if t, ok := str(b["thinking"]); ok && t != "" {
					blocks = append(blocks, api.ThinkingBlock(p.scrubber.Scrub(t)))
				}
			case "toolCall":
				name := strOr(b["name"], "tool")
				args, ok := obj(b["arguments"])
				if !ok {
					args = map[string]any{}
				}
				blocks = append(blocks, api.ToolCallBlock(strOr(b["id"], ""), name,
					Summary(name, args, p.scrubber), InputString(args, p.scrubber)))
			}
		}
		return p.appendAssistant(id, at, blocks)
	case "toolResult":
		isErr, _ := boolean(message["isError"])
		return p.attachResults([]api.Block{api.ToolResultBlock(
			strOr(message["toolCallId"], ""), isErr, Preview(resultText(content), p.scrubber))})
	}
	return nil
}

// MARK: codex

// codex rollouts: only `event_msg` / `item_completed` items (what codex's own UI shows).
// The raw `response_item`s repeat them with injected context and JS tool wrappers.
func (p *Parser) consumeCodex(o map[string]any) *api.Message {
	if strOr(o["type"], "") != "event_msg" {
		return nil
	}
	payload, ok := obj(o["payload"])
	if !ok || strOr(payload["type"], "") != "item_completed" {
		return nil
	}
	item, ok := obj(payload["item"])
	if !ok {
		return nil
	}
	typ, ok := str(item["type"])
	if !ok {
		return nil
	}
	id := idOr(item["id"])
	at := createdAt(o["timestamp"])
	p.currentDate = lineDate(o["timestamp"])

	texts := func() []string {
		items, _ := objects(item["content"])
		var out []string
		for _, b := range items {
			if t, ok := str(b["text"]); ok {
				out = append(out, t)
			}
		}
		return out
	}
	switch typ {
	case "UserMessage":
		return p.appendUser(id, at, []string{strings.Join(texts(), "\n")})
	case "AgentMessage":
		t := strings.Join(texts(), "\n")
		if t == "" {
			return nil
		}
		return p.appendAssistant(id, at, []api.Block{api.TextBlock(p.scrubber.Scrub(t))})
	case "Reasoning":
		summary, _ := stringList(item["summary_text"])
		t := strings.Join(summary, "\n\n")
		if t == "" {
			return nil
		}
		return p.appendAssistant(id, at, []api.Block{api.ThinkingBlock(p.scrubber.Scrub(t))})
	default:
		blocks := p.codexTool(typ, item, id)
		if len(blocks) == 0 {
			return nil
		}
		return p.appendAssistant(id, at, blocks)
	}
}

// codexTool is a finished codex tool item as a toolCall plus its toolResult.
func (p *Parser) codexTool(typ string, item map[string]any, id string) []api.Block {
	stripFile := func(s string) string { return strings.TrimPrefix(s, "file://") }
	failed := false
	if s, ok := str(item["status"]); ok {
		failed = s == "failed" || s == "declined"
	}
	pair := func(name, summary string, input any, output string, isErr bool) []api.Block {
		return []api.Block{
			api.ToolCallBlock(id, name, Truncate(p.scrubber.Scrub(summary), 120), InputString(input, p.scrubber)),
			api.ToolResultBlock(id, isErr, Preview(output, p.scrubber)),
		}
	}
	switch typ {
	case "CommandExecution":
		argv, _ := stringList(item["command"])
		// `["/bin/zsh", "-lc", "<script>"]` → the script.
		cmd := strings.Join(argv, " ")
		if len(argv) >= 3 && (argv[1] == "-lc" || argv[1] == "-c") {
			cmd = argv[2]
		}
		input := map[string]any{"command": cmd}
		if cwd, ok := str(item["cwd"]); ok {
			input["cwd"] = stripFile(cwd)
		}
		exit, _ := integer(item["exit_code"])
		output, ok := str(item["aggregated_output"])
		if !ok {
			var parts []string
			for _, k := range []string{"stdout", "stderr"} {
				if s, ok := str(item[k]); ok {
					parts = append(parts, s)
				}
			}
			output = strings.Join(parts, "\n")
		}
		return pair("Shell", "Ran "+FirstLine(cmd), input, output, failed || exit != 0)
	case "FileChange":
		changes, _ := obj(item["changes"])
		paths := make([]string, 0, len(changes))
		for k := range changes {
			paths = append(paths, k)
		}
		sort.Strings(paths)
		summary := fmt.Sprintf("Edited %d files", len(paths))
		if len(paths) == 1 {
			summary = "Edited " + p.scrubber.Scrub(paths[0])
		}
		var diffs []string
		for _, path := range paths {
			c, ok := obj(changes[path])
			if !ok {
				continue
			}
			body, ok := str(c["unified_diff"])
			if !ok {
				body, ok = str(c["content"])
			}
			if !ok {
				body = strOr(c["type"], "")
			}
			diffs = append(diffs, path+"\n"+body)
		}
		files := make([]any, len(paths))
		for i, path := range paths {
			files[i] = path
		}
		return pair("Edit", summary, map[string]any{"files": files}, strings.Join(diffs, "\n"), failed)
	case "McpToolCall":
		var parts []string
		for _, k := range []string{"server", "tool"} {
			if s, ok := str(item[k]); ok {
				parts = append(parts, s)
			}
		}
		name := strings.Join(parts, ".")
		result, _ := obj(item["result"])
		content, _ := objects(result["content"])
		var texts []string
		for _, c := range content {
			if t, ok := str(c["text"]); ok {
				texts = append(texts, t)
			}
		}
		args, ok := item["arguments"]
		if !ok {
			args = map[string]any{}
		}
		callName := name
		if callName == "" {
			callName = "MCP"
		}
		return pair(callName, "Called "+name, args, strings.Join(texts, "\n"), failed || isTrue(result["isError"]))
	case "Extension":
		query, ok := str(item["query"])
		if !ok {
			query = ""
			if action, ok := obj(item["action"]); ok {
				if qs, ok := stringList(action["queries"]); ok && len(qs) > 0 {
					query = qs[0]
				}
			}
		}
		results, _ := objects(item["results"])
		var domains []string
		for _, r := range results {
			if d, ok := str(r["domain"]); ok {
				domains = append(domains, d)
			}
		}
		kind := strOr(item["kind"], "extension")
		name, summary := kind, kind
		if kind == "web.search" {
			name, summary = "WebSearch", "Searched the web for "+query
		}
		return pair(name, summary, map[string]any{"query": query}, strings.Join(domains, "\n"), failed)
	case "ImageView":
		path := stripFile(strOr(item["path"], ""))
		return pair("ViewImage", "Viewed "+path, map[string]any{"path": path}, "", false)
	}
	return nil
}

// MARK: Building

func (p *Parser) appendAssistant(id, at string, blocks []api.Block) *api.Message {
	if len(blocks) == 0 {
		return nil
	}
	if n := len(p.messages); n > 0 && p.messages[n-1].Role == api.RoleAssistant {
		last := &p.messages[n-1]
		last.Blocks = append(last.Blocks, blocks...)
		return last
	}
	p.messages = append(p.messages, api.Message{ID: id, Role: api.RoleAssistant, CreatedAt: at, Blocks: blocks})
	return &p.messages[len(p.messages)-1]
}

func (p *Parser) appendUser(id, at string, texts []string) *api.Message {
	var kept []string
	for _, t := range texts {
		trimmed := strings.TrimSpace(t)
		if trimmed == "" || hasAnyPrefix(trimmed, droppedUserPrefixes) {
			continue
		}
		kept = append(kept, t)
	}
	if len(kept) == 0 {
		return nil
	}
	var blocks []api.Block
	add := func(a Attached) {
		for _, f := range a.Files {
			blocks = append(blocks, api.AttachmentBlock(f.ID, f.Name, f.Kind))
		}
		if a.Text != "" {
			blocks = append(blocks, api.TextBlock(p.scrubber.Scrub(a.Text)))
		}
	}
	for _, t := range kept {
		if cmd, ok := shellInput(strings.TrimSpace(t)); ok {
			blocks = append(blocks, api.TextBlock(p.scrubber.Scrub("! "+cmd)))
			continue
		}
		// What the bridge sent, even though Claude Code rewrote the text.
		if p.uploads != nil {
			if sent, ok := p.uploads.LookupSent(t, p.currentDate); ok {
				add(sent)
				continue
			}
		}
		t = StripPastedContent(t)
		// Before scrubbing: the marker's absolute paths identify our uploads.
		if p.uploads != nil {
			if parsed, ok := p.uploads.ParseMarker(t); ok {
				add(parsed)
				continue
			}
		}
		blocks = append(blocks, api.TextBlock(p.scrubber.Scrub(t)))
	}
	p.messages = append(p.messages, api.Message{ID: id, Role: api.RoleUser, CreatedAt: at, Blocks: blocks})
	return &p.messages[len(p.messages)-1]
}

// attachResults: tool results belong to the assistant message that made the call; never a user bubble.
func (p *Parser) attachResults(results []api.Block) *api.Message {
	n := len(p.messages)
	if n == 0 || p.messages[n-1].Role != api.RoleAssistant {
		return nil
	}
	last := &p.messages[n-1]
	last.Blocks = append(last.Blocks, results...)
	return last
}

func hasAnyPrefix(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

func resultText(content any) string {
	if s, ok := str(content); ok {
		return s
	}
	arr, ok := objects(content)
	if !ok {
		return ""
	}
	var out []string
	for _, b := range arr {
		switch strOr(b["type"], "") {
		case "text":
			if t, ok := str(b["text"]); ok {
				out = append(out, t)
			}
		case "image":
			out = append(out, "[image]")
		}
	}
	return strings.Join(out, "\n")
}
