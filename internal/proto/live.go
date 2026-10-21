package proto

import (
	"time"

	"github.com/charmbracelet/crush/internal/agent"
)

// Live work, ownership and the smaller gaps (engine-design.md 5.3, 5.4,
// 5.5 and 5.9).

// Helper is a sub-agent working in the background for a chat.
type Helper struct {
	ID             string    `json:"id"`
	SessionID      string    `json:"session_id"`       // the chat that started it
	ChildSessionID string    `json:"child_session_id"` // its own transcript; read it with the existing session and message routes
	Name           string    `json:"name"`
	CLI            string    `json:"cli"`
	Model          string    `json:"model"`
	Effort         string    `json:"effort,omitempty"`
	Fast           bool      `json:"fast,omitempty"`
	ReadOnly       bool      `json:"read_only,omitempty"`
	Status         string    `json:"status"` // "running" | "done" | "failed" | "stopped"
	Started        time.Time `json:"started"`
	Ended          time.Time `json:"ended,omitzero"`
	Delivered      bool      `json:"delivered,omitempty"`
	StartedBy      string    `json:"started_by,omitempty"` // owning app's client ID (5.5); "" = no app
}

// HelperFromTask is the wire form of a helper.
func HelperFromTask(t agent.Task) Helper {
	return Helper{
		ID:             t.ID,
		SessionID:      t.SessionID,
		ChildSessionID: t.ChildID,
		Name:           t.Name,
		CLI:            t.CLI,
		Model:          t.Model,
		Effort:         t.Effort,
		Fast:           t.Fast,
		ReadOnly:       t.ReadOnly,
		Status:         string(t.Status),
		Started:        t.Started,
		Ended:          t.Ended,
		Delivered:      t.Delivered,
		StartedBy:      t.StartedBy,
	}
}

// Job is a command running in the background for a chat.
type Job struct {
	PID       int       `json:"pid,omitempty"`    // 0 for managed jobs without a process of their own
	JobID     string    `json:"job_id,omitempty"` // managed job (`crush bg`, a backgrounded bash call)
	Command   string    `json:"command"`
	Name      string    `json:"name,omitempty"`
	Started   time.Time `json:"started"`
	SessionID string    `json:"session_id"`
	StartedBy string    `json:"started_by,omitempty"` // 5.5
}

// JobRef names one job to kill.
type JobRef struct {
	JobID string `json:"job_id,omitempty"` // exactly one of the two is set
	PID   int    `json:"pid,omitempty"`
}

// BackgroundResult answers a request to move the chat's running command to
// the background.
type BackgroundResult struct {
	Moved bool `json:"moved"`
}

// AgentCapabilities tells an app what the current coder model can do.
type AgentCapabilities struct {
	Kind          string `json:"kind"`           // catwalk type of the current coder model; "" when none
	CanBackground bool   `json:"can_background"` // cliagent.CanBackground(kind)

	// What the coder model supports, as the terminal's switches read it
	// (config.SupportsFastMode, SupportsUltracode, SupportsThinkingToggle).
	SupportsFast           bool `json:"supports_fast,omitempty"`
	SupportsUltracode      bool `json:"supports_ultracode,omitempty"`
	SupportsThinkingToggle bool `json:"supports_thinking_toggle,omitempty"`
}

// ShellOutput is live output of a `!` command, sent to the app that ran it.
type ShellOutput struct {
	RequestID string `json:"request_id"`
	SessionID string `json:"session_id"`
	Seq       int    `json:"seq"`   // 0, 1, 2, … per request
	Chunk     string `json:"chunk"` // output since the previous event; whole UTF-8 characters only
}

// EnvCheck asks which environment variables are set.
type EnvCheck struct {
	Names []string `json:"names"` // bare names, e.g. "OPENAI_API_KEY"; at most 64
}

// EnvCheckResult tells, per name, whether it is set. It never holds values.
type EnvCheckResult struct {
	Set map[string]bool `json:"set"`
}

// MemoryProgram is one of the programs using the most memory.
type MemoryProgram struct {
	Name  string `json:"name"`
	Bytes uint64 `json:"bytes"`
}

// MemoryStatus is the computer's memory use and the memory box's limit.
type MemoryStatus struct {
	Supported     bool            `json:"supported"` // false where memory can't be read (not Linux)
	Percent       int             `json:"percent"`
	High          bool            `json:"high"`
	Episode       int             `json:"episode"`
	Top           []MemoryProgram `json:"top,omitempty"`     // only while high
	Message       string          `json:"message,omitempty"` // Status.Message() while high
	LimitBytes    uint64          `json:"limit_bytes"`       // saved memory-box limit; 0 = none
	BoxUsedBytes  uint64          `json:"box_used_bytes"`    // memlimit.Usage(memlimit.Slice); 0 = unknown
	TotalRAMBytes uint64          `json:"total_ram_bytes"`
}

// ClaimResult tells whether this caller may show the memory pop-up.
type ClaimResult struct {
	Claimed bool `json:"claimed"`
}

// MemoryLimitRequest sets the memory box's limit.
type MemoryLimitRequest struct {
	Value string `json:"value"` // as typed: "", "0", "8", "8 GB", "7.5"
}

// MemoryLimitResult is the outcome of setting the limit.
type MemoryLimitResult struct {
	LimitBytes uint64 `json:"limit_bytes"`
	Applied    bool   `json:"applied"`
	Notice     string `json:"notice"` // the terminal's exact outcome text (DLG-270)
}

// CLIUpdate is a newer version of an AI tool.
type CLIUpdate struct {
	Name    string `json:"name"`
	Bin     string `json:"bin"`
	Path    string `json:"path"`
	Current string `json:"current"`
	Latest  string `json:"latest"`
}

// CLIUpdateCheck is the result of looking for AI tool updates.
type CLIUpdateCheck struct {
	Skipped bool        `json:"skipped"` // automatic check not due yet (hourly)
	Updates []CLIUpdate `json:"updates"`
	Errors  []string    `json:"errors,omitempty"`
}

// CLIUpdateBins names AI tools by their binary.
type CLIUpdateBins struct {
	Bins []string `json:"bins"`
}

// CLIUpdateResult is the outcome of one AI tool's update.
type CLIUpdateResult struct {
	Name   string `json:"name"`
	Latest string `json:"latest"`
	Error  string `json:"error,omitempty"`
}

// CLIUpdatesInstalled is sent to the app that asked for the updates.
type CLIUpdatesInstalled struct {
	Results []CLIUpdateResult `json:"results"`
}

// CrushRelease is a newer Crush release.
type CrushRelease struct {
	Tag     string `json:"tag"`
	Current string `json:"current"`
	Dir     string `json:"dir"`
}

// CrushUpdateCheck is the result of looking for a Crush update.
type CrushUpdateCheck struct {
	HasCheckout bool          `json:"has_checkout"`      // selfupdate.Dir() != ""
	Release     *CrushRelease `json:"release,omitempty"` // nil: on the latest stable release
	Staged      string        `json:"staged,omitempty"`  // already built (selfupdate.Staged)
	Declined    bool          `json:"declined"`          // skipped before
	Error       string        `json:"error,omitempty"`
}

// CrushTag names a Crush release.
type CrushTag struct {
	Tag string `json:"tag"`
}

// LimitWindow is one usage window of an AI tool's account.
type LimitWindow struct {
	Name     string    `json:"name"` // "5h", "Weekly" or a model name
	Used     float64   `json:"used"` // percent, 0-100
	ResetsAt time.Time `json:"resets_at,omitzero"`
	Model    string    `json:"model,omitempty"`
}

// CLILimits is an AI tool's usage limits as this engine last read them.
type CLILimits struct {
	Kind    string        `json:"kind"`              // catwalk type string of the AI tool
	Account string        `json:"account,omitempty"` // options.accounts ID; empty for the tool's own login
	Refills int           `json:"refills,omitempty"` // free refills left (Codex)
	State   string        `json:"state"`            // "checking" | "ready" | "error"
	ReadAt  time.Time     `json:"read_at,omitzero"` // when this engine got the reading (UTC); zero until the first
	Error   string        `json:"error,omitempty"`
	Windows []LimitWindow `json:"windows"`
}

// LimitsSnapshot holds every AI tool's limits.
type LimitsSnapshot struct {
	CLIs []CLILimits `json:"clis"`
}
