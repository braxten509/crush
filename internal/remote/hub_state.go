package remote

import (
	"bytes"
	"cmp"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/agent"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/fsext"
	"github.com/charmbracelet/crush/internal/permission"
	"github.com/charmbracelet/crush/internal/question"
)

// State views. Field names are the phone protocol; keep them stable.

// Share is how the phone lists this window.
type Share struct {
	Version int    `json:"version"`
	Host    string `json:"host"`
	Port    int    `json:"port"`
	Project string `json:"project"`
	Session string `json:"session,omitempty"`
	Title   string `json:"title,omitempty"`
	Busy    bool   `json:"busy"`
	// NeedsYou is set while a permission prompt or questions wait.
	NeedsYou bool   `json:"needs_you"`
	CLI      string `json:"cli,omitempty"`
	Model    string `json:"model,omitempty"`
	Focused  bool   `json:"focused"`
	Phones   int    `json:"phones"`
	// Launch is the id the phone gave when it started this window from the
	// launcher, so it can open the share it asked for.
	Launch string `json:"launch,omitempty"`
}

type statusView struct {
	HasChat       bool   `json:"has_chat"`
	Busy          bool   `json:"busy"`
	NeedsYou      bool   `json:"needs_you"`
	CanBackground bool   `json:"can_background"`
	Focused       bool   `json:"focused"`
	Activity      string `json:"activity,omitempty"`
}

type permissionView struct {
	ID          string     `json:"id"`
	ToolCallID  string     `json:"tool_call_id"`
	Tool        string     `json:"tool"`
	Label       string     `json:"label"`
	Description string     `json:"description,omitempty"`
	Command     string     `json:"command,omitempty"`
	Path        string     `json:"path,omitempty"`
	Diff        []DiffLine `json:"diff,omitempty"`
}

type taskView struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	CLI     string `json:"cli,omitempty"`
	Model   string `json:"model,omitempty"`
	Status  string `json:"status"`
	Started int64  `json:"started"`
	Ended   int64  `json:"ended,omitempty"`
}

type processView struct {
	PID     int    `json:"pid"`
	Command string `json:"command"`
	Started int64  `json:"started"`
}

type modelView struct {
	Provider string   `json:"provider"`
	CLI      string   `json:"cli"`
	Model    string   `json:"model"`
	Name     string   `json:"name"`
	Effort   string   `json:"effort,omitempty"`
	Efforts  []string `json:"efforts,omitempty"`
	Think    *bool    `json:"think,omitempty"`
	Fast     *bool    `json:"fast,omitempty"`
	Ultra    *bool    `json:"ultracode,omitempty"`
	Yolo     bool     `json:"yolo"`
	Plan     bool     `json:"plan"`
	Images   bool     `json:"images"`
}

type cliView struct {
	Provider string        `json:"provider"`
	Name     string        `json:"name"`
	Models   []modelOption `json:"models"`
}

type modelOption struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Efforts []string `json:"efforts,omitempty"`
}

type usageView struct {
	ContextUsed int64       `json:"context_used"`
	ContextMax  int64       `json:"context_max"`
	Cost        float64     `json:"cost"`
	Limits      []limitView `json:"limits"`
}

type limitView struct {
	Name     string  `json:"name"`
	Left     float64 `json:"left"`
	ResetsAt int64   `json:"resets_at,omitempty"`
}

type commandView struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Alias string `json:"alias,omitempty"`
}

type doneView struct {
	Text      string `json:"text,omitempty"`
	Error     string `json:"error,omitempty"`
	Cancelled bool   `json:"cancelled,omitempty"`
}

type itemsEvent struct {
	Type   string            `json:"type"`
	Upsert []json.RawMessage `json:"upsert,omitempty"`
	Remove []string          `json:"remove,omitempty"`
	Order  []string          `json:"order,omitempty"`
}

func event(name string, v any) []byte {
	b, _ := json.Marshal(v)
	return rawEvent(name, b)
}

func rawEvent(name string, v []byte) []byte {
	var b bytes.Buffer
	b.WriteString(`{"type":"` + name + `","data":`)
	if v == nil {
		b.WriteString("null")
	} else {
		b.Write(v)
	}
	b.WriteByte('}')
	return b.Bytes()
}

// stateLocked computes every field.
func (h *hub) stateLocked(items []Item) map[string][]byte {
	cur := h.presence.SessionID
	out := make(map[string][]byte, len(fieldNames))
	put := func(name string, v any) {
		b, _ := json.Marshal(v)
		out[name] = b
	}

	var perm *permissionView
	if p, ok := h.src.PendingPermission(); ok && p.SessionID == cur && cur != "" {
		perm = permissionOf(p, h.src.WorkingDir())
	}
	var q *question.Request
	if r, ok := h.src.PendingQuestion(); ok && r.SessionID == cur && cur != "" {
		q = &r
	}
	busy := cur != "" && h.src.AgentIsSessionBusy(cur)
	model := h.modelLocked()

	status := statusView{
		HasChat:  cur != "",
		Busy:     busy,
		NeedsYou: perm != nil || q != nil,
		Focused:  h.presence.Focused,
	}
	if len(items) > 0 {
		if last := items[len(items)-1]; last.Kind == "group" && last.Live {
			status.Activity = last.Activity
			if n := len(last.Steps); n > 0 {
				s := last.Steps[n-1]
				status.CanBackground = s.Kind == "bash" && s.State == "running" && h.src.CanBackground(catwalk.Type(model.Provider))
			}
		}
	}
	put("status", status)
	put("share", h.shareLocked())
	queue := []string{}
	if cur != "" {
		queue = append(queue, h.src.AgentQueuedPromptsList(cur)...)
	}
	put("queue", queue)
	put("permission", perm)
	put("question", q)

	tasks := []taskView{}
	if cur != "" {
		for _, t := range h.src.Tasks(cur) {
			if t.Status != agent.TaskRunning && time.Since(t.Ended) >= taskLinger {
				continue
			}
			tv := taskView{ID: t.ID, Name: t.Name, CLI: t.CLI, Model: t.Model, Status: string(t.Status), Started: t.Started.Unix()}
			if !t.Ended.IsZero() {
				tv.Ended = t.Ended.Unix()
			}
			tasks = append(tasks, tv)
		}
	}
	put("tasks", tasks)
	procs := []processView{}
	for _, p := range h.procs {
		procs = append(procs, processView{PID: p.PID, Command: p.Command, Started: p.Started.Unix()})
	}
	put("processes", procs)
	put("model", model)
	put("clis", h.clisLocked())
	put("usage", h.usageLocked(model))
	put("commands", h.commandsLocked(model))
	return out
}

func (h *hub) shareLocked() Share {
	cur := h.presence.SessionID
	s := Share{
		Version: ProtocolVersion,
		Host:    h.host,
		Port:    h.port,
		Project: fsext.PrettyPath(h.src.WorkingDir()),
		Session: cur,
		Focused: h.presence.Focused,
		Phones:  len(h.clients),
		Launch:  h.launch,
	}
	if cur != "" {
		s.Title = h.sess.Title
		s.Busy = h.src.AgentIsSessionBusy(cur)
		if p, ok := h.src.PendingPermission(); ok && p.SessionID == cur {
			s.NeedsYou = true
		}
		if q, ok := h.src.PendingQuestion(); ok && q.SessionID == cur {
			s.NeedsYou = true
		}
	}
	m := h.modelLocked()
	s.CLI, s.Model = m.CLI, m.Name
	return s
}

// chatModelLocked returns the model the shared chat owns, or the last pick
// with no chat open.
func (h *hub) chatModelLocked(cfg *config.Config) (config.SelectedModel, bool) {
	if h.presence.SessionID == "" {
		return config.NewChatModel(cfg)
	}
	var selected config.SelectedModel
	if h.sess.ID != h.presence.SessionID || len(h.sess.Model) == 0 || json.Unmarshal(h.sess.Model, &selected) != nil {
		return config.SelectedModel{}, false
	}
	return selected, selected.Provider != ""
}

func (h *hub) modelLocked() modelView {
	cfg := h.src.Config()
	mv := modelView{Yolo: h.src.PermissionSkipRequests(), Plan: h.presence.Plan}
	if cfg == nil {
		return mv
	}
	selected, ok := h.chatModelLocked(cfg)
	if !ok {
		return mv
	}
	mv.Provider, mv.Model = selected.Provider, selected.Model
	m := cfg.GetModel(selected.Provider, selected.Model)
	if p, ok := cfg.Providers.Get(selected.Provider); ok {
		mv.CLI = cmp.Or(p.Name, p.ID)
		if m != nil && config.SupportsFastMode(p, *m) {
			fast := selected.ServiceTier == "fast"
			mv.Fast = &fast
		}
		if m != nil && config.SupportsUltracode(p, *m) {
			ultra := selected.Ultracode
			mv.Ultra = &ultra
		}
		if m != nil && m.CanReason && len(m.ReasoningLevels) == 0 && config.SupportsThinkingToggle(p, *m) {
			think := selected.Think
			mv.Think = &think
		}
	}
	if m != nil {
		mv.Name = cmp.Or(m.Name, m.ID)
		mv.Images = m.SupportsImages
		if m.CanReason && len(m.ReasoningLevels) > 0 {
			mv.Efforts = m.ReasoningLevels
			mv.Effort = cmp.Or(selected.ReasoningEffort, m.DefaultReasoningEffort)
		}
	}
	mv.Name = cmp.Or(mv.Name, mv.Model)
	return mv
}

func (h *hub) clisLocked() []cliView {
	cfg := h.src.Config()
	out := []cliView{}
	if cfg == nil || cfg.Providers == nil {
		return out
	}
	for id, p := range cfg.Providers.Seq2() {
		models := p.AvailableModels()
		if p.Disable || len(models) == 0 {
			continue
		}
		cv := cliView{Provider: id, Name: cmp.Or(p.Name, id)}
		for _, m := range models {
			if m.ID == "" {
				continue
			}
			opt := modelOption{ID: m.ID, Name: cmp.Or(m.Name, m.ID)}
			if m.CanReason {
				opt.Efforts = m.ReasoningLevels
			}
			cv.Models = append(cv.Models, opt)
		}
		if len(cv.Models) > 0 {
			out = append(out, cv)
		}
	}
	slices.SortStableFunc(out, func(a, b cliView) int { return strings.Compare(a.Name, b.Name) })
	return out
}

func (h *hub) usageLocked(m modelView) usageView {
	u := usageView{Limits: []limitView{}}
	if h.presence.SessionID != "" {
		u.ContextUsed = h.sess.PromptTokens + h.sess.CompletionTokens
		u.Cost = h.sess.Cost
	}
	cfg := h.src.Config()
	if cfg == nil {
		return u
	}
	if selected, ok := h.chatModelLocked(cfg); ok {
		if cm := cfg.GetModel(selected.Provider, selected.Model); cm != nil {
			u.ContextMax = cm.ContextWindow
		}
		if p, ok := cfg.Providers.Get(selected.Provider); ok && config.IsCLIProviderType(p.Type) {
			for _, l := range h.src.Limits(p.Type, m.Model) {
				lv := limitView{Name: l.Name, Left: l.Left()}
				if !l.ResetsAt.IsZero() && time.Now().Before(l.ResetsAt) {
					lv.ResetsAt = l.ResetsAt.Unix()
				}
				u.Limits = append(u.Limits, lv)
			}
		}
	}
	return u
}

func (h *hub) commandsLocked(m modelView) []commandView {
	out := []commandView{}
	if h.presence.SessionID != "" {
		out = append(out, commandView{ID: "summarize", Label: "Summarize session", Alias: "compact"})
	}
	if m.Fast != nil {
		label := "Turn on FAST mode"
		if *m.Fast {
			label = "Turn off FAST mode"
		}
		out = append(out, commandView{ID: "fast", Label: label, Alias: "fast"})
	}
	if m.Ultra != nil {
		label := "Turn on Ultracode"
		if *m.Ultra {
			label = "Turn off Ultracode"
		}
		out = append(out, commandView{ID: "ultracode", Label: label, Alias: "ultracode"})
	}
	if m.Think != nil {
		label := "Turn on thinking"
		if *m.Think {
			label = "Turn off thinking"
		}
		out = append(out, commandView{ID: "think", Label: label, Alias: "think"})
	}
	yolo := "Turn on YOLO mode"
	if m.Yolo {
		yolo = "Turn off YOLO mode"
	}
	out = append(out, commandView{ID: "yolo", Label: yolo, Alias: "yolo"})
	mode := "Switch to plan mode"
	if m.Plan {
		mode = "Switch to code mode"
	}
	out = append(out, commandView{ID: "mode", Label: mode, Alias: "plan"})
	return out
}

// permissionOf describes a permission prompt the way the TUI dialog does:
// the command for shell calls, a diff for file changes.
func permissionOf(p permission.PermissionRequest, wd string) *permissionView {
	kind, label := toolKind(p.ToolName)
	v := &permissionView{
		ID:          p.ID,
		ToolCallID:  p.ToolCallID,
		Tool:        kind,
		Label:       label,
		Description: p.Description,
	}
	var params map[string]any
	if b, err := json.Marshal(p.Params); err == nil {
		_ = json.Unmarshal(b, &params)
	}
	str := func(k string) string { s, _ := params[k].(string); return s }
	v.Command = str("command")
	if fp := cmp.Or(str("file_path"), str("path")); fp != "" {
		v.Path = shortPath(fp, wd)
	} else if p.Path != "" && p.Path != wd {
		v.Path = shortPath(p.Path, wd)
	}
	switch {
	case params["old_content"] != nil || params["new_content"] != nil:
		v.Diff = unifiedLines(str("old_content"), str("new_content"))
	case str("diff") != "":
		v.Diff = parseUnified(str("diff"))
	case str("content") != "":
		v.Diff = unifiedLines("", str("content"))
	}
	if len(v.Diff) > detailMaxLines {
		v.Diff = v.Diff[:detailMaxLines]
	}
	return v
}

func clipText(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	cut := s[:n]
	for len(cut) > 0 && cut[len(cut)-1]&0xC0 == 0x80 {
		cut = cut[:len(cut)-1]
	}
	if len(cut) > 0 && cut[len(cut)-1] >= 0xC0 {
		cut = cut[:len(cut)-1]
	}
	return cut + "…"
}
