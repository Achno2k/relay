package controls

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"relay/internal/controls/agentcli"
)

type PiModel struct {
	Provider string
	ID       string
	Thinking bool
}

func (m PiModel) Full() string { return m.Provider + "/" + m.ID }

type CodexModel struct {
	Slug          string
	DisplayName   string
	Visible       bool
	Efforts       []string
	DefaultEffort *string
}

type PiSettings struct {
	DefaultModel    *string // provider/id
	DefaultThinking *string
	EnabledModels   []string // patterns as written
}

// Runner runs a command and returns stdout, or nil; injectable for tests.
type Runner func(args []string) []byte

// Catalogs holds model lists that come from the agents themselves (`pi --list-models`,
// `codex debug models`), cached for 10 minutes per kind, plus their saved defaults.
type Catalogs struct {
	Run               Runner
	PiSettingsPath    string
	PiModelsStorePath string
	ClaudeSettings    string
	CodexConfig       string
	CodexSessions     string

	mu       sync.Mutex
	learned  map[string][]string // levels pi reported itself ("Available levels: …"); win over the store
	cache    map[string]cacheHit
	rollouts map[string]string
}

type cacheHit struct {
	at   time.Time
	data []byte
}

// NewCatalogs uses the live CLIs and the files under the user's home. Fields can be overridden
// before first use.
func NewCatalogs() *Catalogs {
	home, _ := os.UserHomeDir()
	return &Catalogs{
		Run:               Process,
		PiSettingsPath:    filepath.Join(home, ".pi/agent/settings.json"),
		PiModelsStorePath: filepath.Join(home, ".pi/agent/models-store.json"),
		ClaudeSettings:    filepath.Join(home, ".claude/settings.json"),
		CodexConfig:       filepath.Join(home, ".codex/config.toml"),
		CodexSessions:     filepath.Join(home, ".codex/sessions"),
	}
}

func (c *Catalogs) init() {
	if c.learned == nil {
		c.learned = map[string][]string{}
		c.cache = map[string]cacheHit{}
		c.rollouts = map[string]string{}
	}
}

// Process runs a CLI on agentcli's PATH with NO_COLOR=1; nil unless it exits 0.
func Process(args []string) []byte {
	return agentcli.Output(context.Background(), args, "", 30*time.Second, true, "NO_COLOR=1")
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func sortedDirs(dir string) []string {
	entries, _ := os.ReadDir(dir)
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	sort.Slice(out, func(i, j int) bool { return filepath.Base(out[i]) > filepath.Base(out[j]) })
	return out
}

// CodexRollout finds `<sessions>/YYYY/MM/DD/rollout-*-<sessionId>.jsonl`, newest days first.
func (c *Catalogs) CodexRollout(sessionID string) (string, bool) {
	c.mu.Lock()
	c.init()
	hit, ok := c.rollouts[sessionID]
	c.mu.Unlock()
	if ok && exists(hit) {
		return hit, true
	}
	var days []string
	for _, y := range sortedDirs(c.CodexSessions) {
		for _, m := range sortedDirs(y) {
			days = append(days, sortedDirs(m)...)
		}
		if len(days) > 60 {
			break
		}
	}
	if len(days) > 60 {
		days = days[:60]
	}
	for _, d := range days {
		entries, _ := os.ReadDir(d)
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), sessionID+".jsonl") {
				p := filepath.Join(d, e.Name())
				c.mu.Lock()
				c.rollouts[sessionID] = p
				c.mu.Unlock()
				return p, true
			}
		}
	}
	return "", false
}

func (c *Catalogs) cached(key string, args []string) []byte {
	c.mu.Lock()
	c.init()
	hit, ok := c.cache[key]
	c.mu.Unlock()
	if ok && time.Since(hit.at) < 10*time.Minute {
		return hit.data
	}
	data := c.Run(args)
	if len(data) == 0 {
		return nil
	}
	c.mu.Lock()
	c.cache[key] = cacheHit{time.Now(), data}
	c.mu.Unlock()
	return data
}

func (c *Catalogs) Pi() []PiModel {
	data := c.cached("pi", []string{"pi", "--list-models"})
	if data == nil {
		return nil
	}
	return ParsePiList(string(data))
}

func (c *Catalogs) Codex() []CodexModel {
	data := c.cached("codex", []string{"codex", "debug", "models"})
	if data == nil {
		return nil
	}
	return ParseCodexCatalog(data)
}

var piAllLevels = []string{"off", "minimal", "low", "medium", "high", "xhigh", "max"}

// PiSupportedLevels is pi's own rule (getSupportedThinkingLevels): no reasoning → `off` only;
// otherwise every level whose thinkingLevelMap entry isn't null, with xhigh/max only when mapped
// explicitly.
func PiSupportedLevels(reasoning bool, levelMap map[string]any) []string {
	if !reasoning {
		return []string{"off"}
	}
	var out []string
	for _, level := range piAllLevels {
		v, ok := levelMap[level]
		if levelMap == nil || !ok {
			if level != "xhigh" && level != "max" {
				out = append(out, level)
			}
			continue
		}
		if v != nil {
			out = append(out, level)
		}
	}
	return out
}

// PiLevels is the thinking levels pi accepts for `provider/id`, from what pi reported or
// models-store.json. nil = unknown.
func (c *Catalogs) PiLevels(model string) []string {
	c.mu.Lock()
	c.init()
	learned, ok := c.learned[model]
	c.mu.Unlock()
	if ok {
		return learned
	}
	parts := strings.SplitN(model, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil
	}
	data, err := os.ReadFile(c.PiModelsStorePath)
	if err != nil {
		return nil
	}
	var store map[string]any
	if json.Unmarshal(data, &store) != nil {
		return nil
	}
	provider, ok := store[parts[0]].(map[string]any)
	if !ok {
		return nil
	}
	models, ok := provider["models"].([]any)
	if !ok {
		return nil
	}
	for _, raw := range models {
		m, ok := raw.(map[string]any)
		if !ok {
			return nil // Swift's `as? [[String: Any]]` fails as a whole
		}
		if id, _ := m["id"].(string); id == parts[1] {
			reasoning, _ := m["reasoning"].(bool)
			levelMap, _ := m["thinkingLevelMap"].(map[string]any)
			return PiSupportedLevels(reasoning, levelMap)
		}
	}
	return nil
}

func (c *Catalogs) LearnPiLevels(model string, levels []string) {
	c.mu.Lock()
	c.init()
	c.learned[model] = levels
	c.mu.Unlock()
}

func readJSONObject(path string) map[string]any {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var o map[string]any
	if json.Unmarshal(data, &o) != nil {
		return nil
	}
	return o
}

func strp(v any) *string {
	if s, ok := v.(string); ok {
		return &s
	}
	return nil
}

// ClaudeDefaults is Claude's saved default model and effort (`~/.claude/settings.json`).
func (c *Catalogs) ClaudeDefaults() (model, effort *string) {
	o := readJSONObject(c.ClaudeSettings)
	return strp(o["model"]), strp(o["effortLevel"])
}

// CodexDefaults is codex's saved default: top-level `model` / `model_reasoning_effort` in
// config.toml.
func (c *Catalogs) CodexDefaults() (model, effort *string) {
	data, err := os.ReadFile(c.CodexConfig)
	if err != nil {
		return nil, nil
	}
	return ParseCodexConfig(string(data))
}

func ParseCodexConfig(text string) (model, effort *string) {
	for _, raw := range strings.Split(text, "\n") {
		line := trimBlank(raw)
		if strings.HasPrefix(line, "[") {
			break // only top-level keys
		}
		kv := strings.SplitN(line, "=", 2)
		var parts []string
		for _, p := range kv {
			if p != "" {
				parts = append(parts, trimBlank(p))
			}
		}
		if len(parts) != 2 {
			continue
		}
		value := strings.Trim(parts[1], `"'`)
		switch parts[0] {
		case "model":
			model = sp(value)
		case "model_reasoning_effort":
			effort = sp(value)
		}
	}
	return model, effort
}

func (c *Catalogs) PiSettings() PiSettings {
	o := readJSONObject(c.PiSettingsPath)
	if o == nil {
		return PiSettings{EnabledModels: []string{}}
	}
	var model *string
	if m, ok := o["defaultModel"].(string); ok {
		switch p, hasProvider := o["defaultProvider"].(string); {
		case strings.Contains(m, "/"):
			model = sp(m)
		case hasProvider:
			model = sp(p + "/" + m)
		default:
			model = sp(m)
		}
	}
	enabled := []string{}
	if list, ok := o["enabledModels"].([]any); ok {
		for _, e := range list {
			s, ok := e.(string)
			if !ok {
				enabled = []string{}
				break
			}
			enabled = append(enabled, s)
		}
	}
	return PiSettings{DefaultModel: model, DefaultThinking: strp(o["defaultThinkingLevel"]), EnabledModels: enabled}
}

// ParsePiList reads pi's `provider  model  context  max-out  thinking  images` table.
func ParsePiList(text string) []PiModel {
	lines := strings.Split(text, "\n")
	var out []PiModel
	for _, line := range lines[1:] {
		cols := strings.Fields(line)
		if len(cols) < 5 || cols[0] == "provider" {
			continue
		}
		out = append(out, PiModel{Provider: cols[0], ID: cols[1], Thinking: cols[4] == "yes"})
	}
	return out
}

// ParseCodexCatalog reads `codex debug models`, ordered by priority.
func ParseCodexCatalog(data []byte) []CodexModel {
	var o map[string]any
	if json.Unmarshal(data, &o) != nil {
		return nil
	}
	raw, ok := o["models"].([]any)
	if !ok {
		return nil
	}
	models := make([]map[string]any, 0, len(raw))
	for _, r := range raw {
		m, ok := r.(map[string]any)
		if !ok {
			return nil
		}
		models = append(models, m)
	}
	priority := func(m map[string]any) float64 {
		if p, ok := m["priority"].(float64); ok && p == float64(int64(p)) {
			return p
		}
		return 999
	}
	sort.SliceStable(models, func(i, j int) bool { return priority(models[i]) < priority(models[j]) })
	var out []CodexModel
	for _, m := range models {
		slug, ok := m["slug"].(string)
		if !ok {
			continue
		}
		var efforts []string
		if levels, ok := m["supported_reasoning_levels"].([]any); ok {
			for _, l := range levels {
				lm, ok := l.(map[string]any)
				if !ok {
					efforts = nil
					break
				}
				if e, ok := lm["effort"].(string); ok {
					efforts = append(efforts, e)
				}
			}
		}
		name := slug
		if n, ok := m["display_name"].(string); ok {
			name = n
		}
		vis, _ := m["visibility"].(string)
		out = append(out, CodexModel{Slug: slug, DisplayName: name, Visible: vis == "list", Efforts: efforts,
			DefaultEffort: strp(m["default_reasoning_level"])})
	}
	return out
}
