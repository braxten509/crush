package config

import (
	"encoding/json"
	"time"

	"github.com/invopop/jsonschema"
)

type TUIOptions struct {
	CompactMode bool        `json:"compact_mode,omitempty" jsonschema:"description=Enable compact mode for the TUI interface,default=false"`
	DiffMode    string      `json:"diff_mode,omitempty" jsonschema:"description=Diff mode for the TUI interface,enum=unified,enum=split"`
	ActiveTheme string      `json:"active_theme,omitempty" jsonschema:"description=Name of the currently active theme,default=charmtone-panther,example=charmtone-panther,example=gruvbox-dark"`
	Completions Completions `json:"completions,omitzero" jsonschema:"description=Completions UI options"`
	Transparent *bool       `json:"transparent,omitempty" jsonschema:"description=Enable transparent background for the TUI interface,default=false"`
	Scrollbar   string      `json:"scrollbar,omitempty" jsonschema:"description=Chat scrollbar visibility,enum=default,enum=always,enum=never,default=default"`
	Mouse       *bool       `json:"mouse,omitempty" jsonschema:"description=Enable terminal mouse capture for selection\\, clicks\\, and scrolling in the TUI. Disable to let the terminal emulator or tmux handle text selection and copy/paste,default=true"`
	ExitBanner  ExitBanner  `json:"exit_banner,omitempty" jsonschema:"description=Exit banner style after quitting Crush,enum=default,enum=compact,enum=none,default=default"`
	// ComposerFooter lists the stats shown under the composer, in a fixed
	// order; unset shows the defaults. See the Customize Composer command.
	ComposerFooter []string `json:"composer_footer,omitempty" jsonschema:"description=Stats shown under the composer: model\\, settings\\, tokens\\, context\\, limit_5h\\, limit_weekly\\, limit_other\\, resets. Unset shows all but resets"`
	// ComposerFocusOnly keeps keyboard focus on the composer.
	ComposerFocusOnly bool `json:"composer_focus_only,omitempty" jsonschema:"description=Keep keyboard focus on the composer: Tab doesn't move focus and clicking the chat or sidebar doesn't take it. The chat still works with the mouse,default=false"`
	// ThoughtLine shows the newest line of the model's thinking next to
	// the busy status. Unset means on.
	ThoughtLine *bool `json:"thought_line,omitempty" jsonschema:"description=Show the newest line of the model's thinking next to the busy status,default=true"`
}

// ShowsThoughtLine reports whether the busy status shows the newest line
// of the model's thinking. The nil receiver and the unset pointer mean on.
func (t *TUIOptions) ShowsThoughtLine() bool {
	return t == nil || t.ThoughtLine == nil || *t.ThoughtLine
}

// FocusesComposerOnly reports whether keyboard focus stays on the composer.
// The nil receiver means the default (focus can move).
func (t *TUIOptions) FocusesComposerOnly() bool {
	return t != nil && t.ComposerFocusOnly
}

// IsTransparent reports whether the TUI draws a transparent background. The
// nil receiver and the unset pointer both mean opaque, so callers can ask
// without unwrapping either.
func (t *TUIOptions) IsTransparent() bool {
	return t != nil && t.Transparent != nil && *t.Transparent
}

// UnmarshalJSON tolerates the legacy string form of the "theme" field.
// Older Crush builds stored the selected theme as `"theme":
// "gruvbox-dark"`. When that string is encountered it is promoted to the
// active theme name so existing configs keep loading.
func (t *TUIOptions) UnmarshalJSON(data []byte) error {
	type tuiOptionsAlias TUIOptions
	var loose struct {
		tuiOptionsAlias
		Theme json.RawMessage `json:"theme"`
	}
	if err := json.Unmarshal(data, &loose); err != nil {
		return err
	}
	*t = TUIOptions(loose.tuiOptionsAlias)
	if len(loose.Theme) == 0 || string(loose.Theme) == "null" {
		return nil
	}

	var legacyName string
	if err := json.Unmarshal(loose.Theme, &legacyName); err != nil {
		// Legacy inline theme maps are ignored now that files are the only
		// palette source, but they must remain loadable during migration.
		var legacyThemes map[string]json.RawMessage
		return json.Unmarshal(loose.Theme, &legacyThemes)
	}
	if legacyName != "" && t.ActiveTheme == "" {
		t.ActiveTheme = legacyName
	}
	return nil
}

// Completions defines options for the completions UI.
type Completions struct {
	MaxDepth *int `json:"max_depth,omitempty" jsonschema:"description=Maximum depth for the ls tool,default=0,example=10"`
	MaxItems *int `json:"max_items,omitempty" jsonschema:"description=Maximum number of items to return for the ls tool,default=1000,example=100"`
}

// Limits returns the configured completion limits. Zero means the user has not
// pinned that limit, and callers fall back to their own built-in cap.
func (c Completions) Limits() (depth, items int) {
	return ptrValOr(c.MaxDepth, 0), ptrValOr(c.MaxItems, 0)
}

// Diff mode options.
const (
	DiffModeUnified = "unified" // Inline unified diffs
	DiffModeSplit   = "split"   // Side-by-side diffs
)

// Scrollbar visibility options.
const (
	ScrollbarDefault = "default" // Auto-hide after 2 seconds
	ScrollbarAlways  = "always"  // Always show when content exceeds viewport
	ScrollbarNever   = "never"   // Never show scrollbar
)

// ExitBanner selects what Crush prints after the TUI exits.
type ExitBanner string

const (
	// ExitBannerDefault renders the full ASCII art logo with padding. It is
	// also what the zero value and any unrecognized value fall back to.
	ExitBannerDefault ExitBanner = "default"
	// ExitBannerCompact renders only the session and resume lines, with no
	// logo and no padding. With no active session it renders nothing at all,
	// so Crush exits silently.
	ExitBannerCompact ExitBanner = "compact"
	// ExitBannerNone renders nothing.
	ExitBannerNone ExitBanner = "none"
)

type Permissions struct {
	AllowedTools []string `json:"allowed_tools,omitempty" jsonschema:"description=List of tools that don't require permission prompts,example=bash,example=view"`
}

type TrailerStyle string

const (
	TrailerStyleNone         TrailerStyle = "none"
	TrailerStyleCoAuthoredBy TrailerStyle = "co-authored-by"
	TrailerStyleAssistedBy   TrailerStyle = "assisted-by"
)

type Attribution struct {
	TrailerStyle  TrailerStyle `json:"trailer_style,omitempty" jsonschema:"description=Style of attribution trailer to add to commits,enum=none,enum=co-authored-by,enum=assisted-by,default=assisted-by"`
	CoAuthoredBy  *bool        `json:"co_authored_by,omitempty" jsonschema:"description=Deprecated: use trailer_style instead"`
	GeneratedWith bool         `json:"generated_with,omitempty" jsonschema:"description=Add Generated with Crush line to commit messages and issues and PRs,default=true"`
}

// JSONSchemaExtend marks the co_authored_by field as deprecated in the schema.
func (Attribution) JSONSchemaExtend(schema *jsonschema.Schema) {
	if schema.Properties != nil {
		if prop, ok := schema.Properties.Get("co_authored_by"); ok {
			prop.Deprecated = true
		}
	}
}

type Options struct {
	AutoCompactTokenLimit   int64             `json:"auto_compact_token_limit,omitempty" jsonschema:"description=Fallback compaction threshold for models without native compaction; defaults to 400000,minimum=1000"`
	Accounts                []Account         `json:"accounts,omitempty" jsonschema:"description=More Claude Code and Codex sign-ins, each in its own folder; the tools' own logins are account 1 and are not listed"`
	OwnAccountNames         map[string]string `json:"own_account_names,omitempty" jsonschema:"description=Names given to each tool's own login (account 1) by tool (claude-code or codex-cli); an unnamed login shows the email it signed in with"`
	Fallback                *FallbackOrder    `json:"fallback,omitempty" jsonschema:"description=The fallback order: which account and model a chat moves to when one runs out"`
	MemoryLimitGB           *float64          `json:"memory_limit_gb,omitempty" jsonschema:"description=Most memory (RAM and swap together) all Crush windows and everything they start may use, in GB; 0 means no limit; defaults to half the computer's RAM (Linux only),minimum=0"`
	ContextPaths            []string          `json:"context_paths,omitempty" jsonschema:"description=Paths to files containing context information for the AI,example=.cursorrules,example=CRUSH.md"`
	DisableInstructionFiles bool              `json:"disable_instruction_files,omitempty" jsonschema:"description=Use shared Crush instructions and memory instead of personal and project instruction files"`
	GlobalContextPaths      []string          `json:"global_context_paths,omitempty" jsonschema:"description=Paths to files containing global context information for the AI,default=~/.config/crush/CRUSH.md,default=~/.config/AGENTS.md"`
	SkillsPaths             []string          `json:"skills_paths,omitempty" jsonschema:"description=Paths to directories containing Agent Skills (folders with SKILL.md files),example=~/.config/crush/skills,example=./skills"`
	TUI                     *TUIOptions       `json:"tui,omitempty" jsonschema:"description=Terminal user interface options"`
	Debug                   bool              `json:"debug,omitempty" jsonschema:"description=Enable debug logging,default=false"`
	DebugLSP                bool              `json:"debug_lsp,omitempty" jsonschema:"description=Enable debug logging for LSP servers,default=false"`
	DisableAutoSummarize    bool              `json:"disable_auto_summarize,omitempty" jsonschema:"description=Disable automatic conversation summarization,default=false"`
	// DataDirectory is where Crush keeps per-project state such as
	// the SQLite database and workspace overrides. Relative paths are
	// resolved against the working directory; absolute paths are used
	// verbatim. After defaulting the stored value is always absolute.
	DataDirectory             string       `json:"data_directory,omitempty" jsonschema:"description=Directory for storing application data. Relative paths are resolved against the working directory; absolute paths are used as-is.,default=.crush,example=.crush"`
	DisabledTools             []string     `json:"disabled_tools,omitempty" jsonschema:"description=List of built-in tools to disable and hide from the agent,example=bash,example=sourcegraph"`
	DisableProviderAutoUpdate bool         `json:"disable_provider_auto_update,omitempty" jsonschema:"description=Disable providers auto-update,default=false"`
	DisableDefaultProviders   bool         `json:"disable_default_providers,omitempty" jsonschema:"description=Ignore all default/embedded providers. When enabled\\, providers must be fully specified in the config file with base_url\\, models\\, and api_key - no merging with defaults occurs,default=false"`
	Attribution               *Attribution `json:"attribution,omitempty" jsonschema:"description=Attribution settings for generated content"`
	DisableMetrics            bool         `json:"disable_metrics,omitempty" jsonschema:"description=Disable sending metrics,default=false"`
	InitializeAs              string       `json:"initialize_as,omitempty" jsonschema:"description=Name of the context file to create/update during project initialization,default=AGENTS.md,example=AGENTS.md,example=CRUSH.md,example=CLAUDE.md,example=docs/LLMs.md"`
	AutoLSP                   *bool        `json:"auto_lsp,omitempty" jsonschema:"description=Automatically setup LSPs based on root markers,default=true"`
	Progress                  *bool        `json:"progress,omitempty" jsonschema:"description=Show indeterminate progress updates during long operations,default=true"`
	Notifications             string       `json:"notifications,omitempty" jsonschema:"description=Notification style to use. Options: auto (default)\\, native\\, osc\\, bell\\, disabled. Auto selects based on environment: native for local sessions\\, osc for SSH (with automatic OSC 99/777 detection).,enum=auto,enum=native,enum=osc,enum=bell,enum=disabled,default=auto"`
	DisabledSkills            []string     `json:"disabled_skills,omitempty" jsonschema:"description=List of skill names to disable and hide from the agent,example=crush-config"`
	CheckInMinutes            *int         `json:"check_in_minutes,omitempty" jsonschema:"description=Minutes between automatic progress check-ins Crush sends the agent while its background jobs and sub-agents run (services excluded). 0 turns them off.,default=2,minimum=0"`
	RequestTimeout            *int         `json:"request_timeout,omitempty" jsonschema:"description=Timeout in seconds for each LLM API request. Streaming responses are aborted only after this much inactivity\\, so slow but active streams are never killed. 0 disables it\\, negative values are invalid.,default=60,example=120,example=300,example=0"`
	// AgentCPUsEnabled keeps Crush and everything it starts on AgentCPUs so
	// the other CPUs stay free for the desktop. See the cpuaffinity package.
	AgentCPUsEnabled bool   `json:"agent_cpus_enabled,omitempty" jsonschema:"description=Run Crush and everything it starts (agents\\, sub-agents\\, background jobs\\, dev servers) on only some CPUs so the rest stay free for the desktop. Linux only,default=false"`
	AgentCPUs        string `json:"agent_cpus,omitempty" jsonschema:"description=CPU list used by agent_cpus_enabled. Empty picks the last group of CPUs sharing a cache (or the upper half of the cores),example=6-11\\,18-23"`
	// RemoteLaunchTarget is what the phone launcher opens: "terminal" (the
	// default) or "desktop" (desktop design 5-6, G-17).
	RemoteLaunchTarget string `json:"remote_launch_target,omitempty" jsonschema:"description=What crush remote launcher opens for the phone: a terminal window or the desktop app,enum=terminal,enum=desktop,default=terminal"`
	// Engine is where a terminal window runs its chats: "shared" (the
	// default) joins the project's engine, which the desktop app uses too;
	// "alone" runs in-process as before (engine-design.md 5.13).
	// CRUSH_ENGINE overrides it.
	Engine string `json:"engine,omitempty" jsonschema:"description=Where a terminal window runs its chats: shared joins the project's engine (so the desktop app and other windows show the same chats live)\\, alone runs them inside the window,enum=shared,enum=alone,default=shared"`
}

// Engine modes (engine-design.md 5.13).
const (
	EngineShared = "shared"
	EngineAlone  = "alone"
)

// DefaultCheckInInterval is how often Crush wakes the agent with a progress
// check-in while background work runs, unless options.check_in_minutes says
// otherwise.
const DefaultCheckInInterval = 2 * time.Minute

// GetCheckInInterval returns the time between progress check-ins, or zero
// when they are off. The nil receiver and the unset field both mean
// DefaultCheckInInterval.
func (o *Options) GetCheckInInterval() time.Duration {
	if o == nil || o.CheckInMinutes == nil {
		return DefaultCheckInInterval
	}
	if *o.CheckInMinutes <= 0 {
		return 0
	}
	return time.Duration(*o.CheckInMinutes) * time.Minute
}

// DefaultRequestTimeout bounds each LLM API request when the user has not
// configured a timeout. Slow or unreachable providers fail after it instead
// of blocking a session forever; streamed responses are only aborted after
// this much inactivity, and users running slow local models can raise or
// disable it via options.request_timeout.
const DefaultRequestTimeout = 2 * time.Minute

// GetRequestTimeout returns the per-request timeout for LLM API calls (a
// hard deadline for non-streaming requests and an idle timeout for
// streams), or zero when disabled. The nil receiver and the unset field
// both mean DefaultRequestTimeout, so callers can ask without unwrapping
// either.
func (o *Options) GetRequestTimeout() time.Duration {
	if o == nil || o.RequestTimeout == nil {
		return DefaultRequestTimeout
	}
	if *o.RequestTimeout <= 0 {
		return 0
	}
	return time.Duration(*o.RequestTimeout) * time.Second
}

type Tools struct {
	Ls   ToolLs   `json:"ls,omitzero"`
	Grep ToolGrep `json:"grep,omitzero"`
	Glob ToolGlob `json:"glob,omitzero"`
}

type ToolLs struct {
	MaxDepth *int `json:"max_depth,omitempty" jsonschema:"description=Maximum depth for the ls tool,default=0,example=10"`
	MaxItems *int `json:"max_items,omitempty" jsonschema:"description=Maximum number of items to return for the ls tool,default=1000,example=100"`
}

// Limits returns the user-defined max-depth and max-items, or their defaults.
func (t ToolLs) Limits() (depth, items int) {
	return ptrValOr(t.MaxDepth, 0), ptrValOr(t.MaxItems, 0)
}

type ToolGrep struct {
	Timeout *time.Duration `json:"timeout,omitempty" jsonschema:"description=Timeout for the grep tool call,default=5s,example=10s"`
}

// GetTimeout returns the user-defined timeout or the default.
func (t ToolGrep) GetTimeout() time.Duration {
	return ptrValOr(t.Timeout, 5*time.Second)
}

type ToolGlob struct {
	Timeout *time.Duration `json:"timeout,omitempty" jsonschema:"description=Timeout for the glob tool call,default=30s,example=10s"`
}

// GetTimeout returns the user-defined timeout or the default.
func (t ToolGlob) GetTimeout() time.Duration {
	return ptrValOr(t.Timeout, 30*time.Second)
}
