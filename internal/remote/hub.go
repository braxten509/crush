package remote

import (
	"bytes"
	"cmp"
	"context"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/agent"
	"github.com/charmbracelet/crush/internal/agent/notify"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/fsext"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/permission"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/charmbracelet/crush/internal/question"
	"github.com/charmbracelet/crush/internal/session"
)

// The hub keeps the phone's view of the window: it follows the chat the
// window shows, recomputes the state when anything changes, and sends each
// phone only what changed since the last send. A phone that connects (or
// reconnects after sleep or a network switch) first gets a full snapshot,
// so nothing is lost while it was away.

// Fields are sent whole whenever they change; items are sent one by one.
var fieldNames = []string{"share", "status", "queue", "permission", "question", "tasks", "processes", "model", "clis", "usage", "commands"}

// liteFields are all a lite client gets: enough to list the share and to
// alert, without the chat itself.
var liteFields = map[string]bool{"share": true, "status": true, "permission": true, "question": true}

const (
	coalesce      = 80 * time.Millisecond
	backstop      = time.Second
	procsInterval = 2 * time.Second
	taskLinger    = 8 * time.Second
	clientBuffer  = 512
)

type hub struct {
	src  Source
	host string
	port int
	wake chan struct{}
	// changes tells the TUI the phone list changed.
	changes chan struct{}

	mu       sync.Mutex
	presence Presence
	loaded   string // session whose messages are in msgs
	sess     session.Session
	msgs     []message.Message
	version  int  // bumped whenever msgs change
	stale    bool // reread msgs: events are lossy under load
	procs    []agent.Process
	clients  map[*client]struct{}
	fields   map[string][]byte
	items    map[string][]byte
	order    []string
	closed   bool

	// The chat list is rebuilt only when its inputs change.
	built   []Item
	builtAt itemsKey
	figures map[string]figures
}

type itemsKey struct {
	session string
	version int
	busy    bool
	waiting string
}

func newHub(src Source, host string, port int) *hub {
	return &hub{
		src:     src,
		host:    host,
		port:    port,
		wake:    make(chan struct{}, 1),
		changes: make(chan struct{}, 1),
		clients: map[*client]struct{}{},
		fields:  map[string][]byte{},
		items:   map[string][]byte{},
	}
}

func (h *hub) run(ctx context.Context) {
	events := h.src.Events(ctx)
	back := time.NewTicker(backstop)
	defer back.Stop()
	procs := time.NewTicker(procsInterval)
	defer procs.Stop()
	timer := time.NewTimer(time.Hour)
	timer.Stop()
	armed := false
	arm := func() {
		if !armed {
			armed = true
			timer.Reset(coalesce)
		}
	}
	h.refreshProcesses()
	h.flush()
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-events:
			if !ok {
				return
			}
			if h.apply(ev.Payload) {
				arm()
			}
		case <-h.wake:
			arm()
		case <-timer.C:
			armed = false
			h.flush()
		case <-back.C:
			h.flush()
		case <-procs.C:
			h.refreshProcesses()
		}
	}
}

// poke schedules a recompute soon.
func (h *hub) poke() {
	select {
	case h.wake <- struct{}{}:
	default:
	}
}

func (h *hub) refreshProcesses() {
	procs := h.src.Processes()
	h.mu.Lock()
	h.procs = procs
	h.mu.Unlock()
	h.poke()
}

func (h *hub) setPresence(p Presence) {
	h.mu.Lock()
	changed := h.presence != p
	h.presence = p
	h.mu.Unlock()
	if changed {
		h.poke()
	}
}

func (h *hub) currentSession() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.presence.SessionID
}

// apply folds one app event into the cached chat. It reports whether the
// phone's view may have changed.
func (h *hub) apply(ev any) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	cur := h.presence.SessionID
	switch e := ev.(type) {
	case pubsub.Event[message.Message]:
		if e.Payload.SessionID != cur || h.loaded != cur {
			return false
		}
		m := e.Payload.Clone()
		i := slices.IndexFunc(h.msgs, func(x message.Message) bool { return x.ID == m.ID })
		switch {
		case e.Type == pubsub.DeletedEvent:
			if i >= 0 {
				h.msgs = slices.Delete(h.msgs, i, i+1)
			}
		case i >= 0:
			if m.UpdatedAt >= h.msgs[i].UpdatedAt {
				h.msgs[i] = m
			}
		default:
			h.msgs = append(h.msgs, m)
			slices.SortStableFunc(h.msgs, func(a, b message.Message) int { return cmp.Compare(a.CreatedAt, b.CreatedAt) })
		}
		h.version++
		return true
	case pubsub.Event[session.Session]:
		if e.Payload.ID == cur {
			h.sess = e.Payload
			return true
		}
		return false
	case pubsub.Event[notify.RunComplete]:
		if e.Payload.SessionID == cur {
			h.stale = true
			h.broadcastLocked(event("done", doneView{
				Text:      clipText(e.Payload.Text, 400),
				Error:     e.Payload.Error,
				Cancelled: e.Payload.Cancelled,
			}), false)
			return true
		}
		return false
	case pubsub.Event[permission.PermissionRequest], pubsub.Event[permission.PermissionNotification],
		pubsub.Event[question.Request], pubsub.Event[question.Notification], pubsub.Event[agent.Task]:
		return true
	}
	return false
}

// flush recomputes the state and sends what changed.
func (h *hub) flush() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	reset := false
	if cur := h.presence.SessionID; cur != h.loaded {
		h.load(cur)
		reset = true
	} else if h.stale && cur != "" {
		h.reloadMessages()
	}
	items := h.itemsLocked()
	st := h.stateLocked(items)

	if reset {
		h.fields = map[string][]byte{}
		h.items = map[string][]byte{}
		h.order = nil
		for name, v := range st {
			h.fields[name] = v
		}
		h.order = make([]string, 0, len(items))
		for _, it := range items {
			b, _ := json.Marshal(it)
			h.items[it.ID] = b
			h.order = append(h.order, it.ID)
		}
		snap, lite := h.snapshotLocked(false), h.snapshotLocked(true)
		for c := range h.clients {
			if c.lite {
				h.sendLocked(c, lite)
			} else {
				h.sendLocked(c, snap)
			}
		}
		return
	}

	for _, name := range fieldNames {
		v := st[name]
		if bytes.Equal(v, h.fields[name]) {
			continue
		}
		h.fields[name] = v
		h.broadcastLocked(rawEvent(name, v), !liteFields[name])
	}

	var upsert []json.RawMessage
	order := make([]string, 0, len(items))
	seen := make(map[string]bool, len(items))
	for _, it := range items {
		b, _ := json.Marshal(it)
		order = append(order, it.ID)
		seen[it.ID] = true
		if !bytes.Equal(b, h.items[it.ID]) {
			h.items[it.ID] = b
			upsert = append(upsert, b)
		}
	}
	var removed []string
	for id := range h.items {
		if !seen[id] {
			removed = append(removed, id)
			delete(h.items, id)
		}
	}
	orderChanged := !slices.Equal(order, h.order)
	if len(upsert) == 0 && len(removed) == 0 && !orderChanged {
		return
	}
	h.order = order
	ev := itemsEvent{Type: "items", Upsert: upsert, Remove: removed}
	if orderChanged {
		ev.Order = order
	}
	b, _ := json.Marshal(ev)
	h.broadcastLocked(b, true)
}

// load replaces the cached chat with the session the window now shows.
func (h *hub) load(sessionID string) {
	h.loaded = sessionID
	h.msgs = nil
	h.version++
	h.figures = map[string]figures{}
	h.sess = session.Session{}
	if sessionID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if s, err := h.src.GetSession(ctx, sessionID); err == nil {
		h.sess = s
	}
	h.reloadMessages()
}

// reloadMessages rereads the cached chat from the database.
func (h *hub) reloadMessages() {
	h.stale = false
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	msgs, err := h.src.ListMessages(ctx, h.loaded)
	if err != nil {
		slog.Warn("Remote: loading the chat failed", "session", h.loaded, "error", err)
		return
	}
	h.msgs = msgs
	h.version++
}

func (h *hub) itemsLocked() []Item {
	cur := h.presence.SessionID
	if cur == "" {
		return nil
	}
	st := chatState{busy: h.src.AgentIsSessionBusy(cur), workingDir: h.src.WorkingDir(), figures: h.figures}
	if p, ok := h.src.PendingPermission(); ok && p.SessionID == cur {
		st.waitingCall = p.ToolCallID
	}
	key := itemsKey{session: cur, version: h.version, busy: st.busy, waiting: st.waitingCall}
	if key != h.builtAt || h.built == nil {
		h.built = buildItems(h.msgs, st)
		h.builtAt = key
	}
	return h.built
}

// snapshotLocked is the state for a phone that just connected; lite leaves
// out the chat.
func (h *hub) snapshotLocked(lite bool) []byte {
	var b bytes.Buffer
	b.WriteString(`{"type":"snapshot"`)
	for _, name := range fieldNames {
		if lite && !liteFields[name] {
			continue
		}
		b.WriteString(`,"` + name + `":`)
		if v := h.fields[name]; v != nil {
			b.Write(v)
		} else {
			b.WriteString("null")
		}
	}
	if lite {
		b.WriteString("}")
		return b.Bytes()
	}
	b.WriteString(`,"items":[`)
	for i, id := range h.order {
		if i > 0 {
			b.WriteByte(',')
		}
		b.Write(h.items[id])
	}
	b.WriteString("]}")
	return b.Bytes()
}

func (h *hub) register(name string, lite bool) (*client, []byte) {
	c := &client{name: name, lite: lite, ch: make(chan []byte, clientBuffer)}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		c.close()
		return c, nil
	}
	if h.loaded != h.presence.SessionID || len(h.fields) == 0 {
		// Nothing computed for this chat yet: compute it now so the
		// snapshot is current.
		h.mu.Unlock()
		h.flush()
		h.mu.Lock()
		if h.closed {
			c.close()
			return c, nil
		}
	}
	h.clients[c] = struct{}{}
	h.phonesChanged()
	snap := h.snapshotLocked(lite)
	// The share lists how many phones are connected; tell the others.
	h.poke()
	return c, snap
}

func (h *hub) unregister(c *client) {
	h.mu.Lock()
	if _, ok := h.clients[c]; ok {
		delete(h.clients, c)
		h.phonesChanged()
	}
	h.mu.Unlock()
	c.close()
	h.poke()
}

// closeAll ends every phone's stream with a stopped event, so phones know
// the share ended rather than dropped, and don't try to reconnect.
func (h *hub) closeAll() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	stopped := event("stopped", nil)
	for c := range h.clients {
		select {
		case c.ch <- stopped:
		default:
		}
		c.close()
		delete(h.clients, c)
	}
}

func (h *hub) phones() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []string
	for c := range h.clients {
		out = append(out, cmp.Or(c.name, "a phone"))
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// broadcastLocked sends b to every phone; chatOnly skips lite clients.
func (h *hub) broadcastLocked(b []byte, chatOnly bool) {
	for c := range h.clients {
		if chatOnly && c.lite {
			continue
		}
		h.sendLocked(c, b)
	}
}

// sendLocked queues b for c, dropping a phone that fell too far behind; it
// reconnects and gets a fresh snapshot.
func (h *hub) sendLocked(c *client, b []byte) {
	select {
	case c.ch <- b:
	default:
		delete(h.clients, c)
		c.close()
		h.phonesChanged()
	}
}

func (h *hub) phonesChanged() {
	select {
	case h.changes <- struct{}{}:
	default:
	}
}

func (h *hub) share() Share {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.shareLocked()
}

func (h *hub) stepDetail(id string) (StepDetail, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return stepDetail(h.msgs, id)
}

// media returns an attachment of a user message (idx "N") or the image of a
// tool result (idx "rN").
func (h *hub) media(msgID, idx string) (string, []byte, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, m := range h.msgs {
		if m.ID != msgID {
			continue
		}
		if rest, ok := strings.CutPrefix(idx, "r"); ok {
			i, err := strconv.Atoi(rest)
			res := m.ToolResults()
			if err != nil || i < 0 || i >= len(res) || res[i].Data == "" {
				return "", nil, false
			}
			data, err := base64.StdEncoding.DecodeString(res[i].Data)
			if err != nil {
				return "", nil, false
			}
			return res[i].MIMEType, data, true
		}
		i, err := strconv.Atoi(idx)
		bins := m.BinaryContent()
		if err != nil || i < 0 || i >= len(bins) {
			return "", nil, false
		}
		return bins[i].MIMEType, bins[i].Data, true
	}
	return "", nil, false
}

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
		Launch:  LaunchID(),
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

func (h *hub) modelLocked() modelView {
	cfg := h.src.Config()
	mv := modelView{Yolo: h.src.PermissionSkipRequests(), Plan: h.presence.Plan}
	if cfg == nil {
		return mv
	}
	agentCfg, ok := cfg.Agents[config.AgentCoder]
	if !ok {
		return mv
	}
	selected := cfg.Models[agentCfg.Model]
	mv.Provider, mv.Model = selected.Provider, selected.Model
	if p := cfg.GetProviderForModel(agentCfg.Model); p != nil {
		mv.CLI = cmp.Or(p.Name, p.ID)
		if model := cfg.GetModelByType(agentCfg.Model); model != nil && config.SupportsFastMode(*p, *model) {
			fast := selected.ServiceTier == "fast"
			mv.Fast = &fast
		}
		if model := cfg.GetModelByType(agentCfg.Model); model != nil && config.SupportsUltracode(*p, *model) {
			ultra := selected.Ultracode
			mv.Ultra = &ultra
		}
	}
	if m := cfg.GetModelByType(agentCfg.Model); m != nil {
		mv.Name = cmp.Or(m.Name, m.ID)
		mv.Images = m.SupportsImages
		if m.CanReason {
			if len(m.ReasoningLevels) > 0 {
				mv.Efforts = m.ReasoningLevels
				mv.Effort = cmp.Or(selected.ReasoningEffort, m.DefaultReasoningEffort)
			} else if provider := cfg.GetProviderForModel(agentCfg.Model); provider != nil && config.SupportsThinkingToggle(*provider, *m) {
				think := selected.Think
				mv.Think = &think
			}
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
	if agentCfg, ok := cfg.Agents[config.AgentCoder]; ok {
		if cm := cfg.GetModelByType(agentCfg.Model); cm != nil {
			u.ContextMax = cm.ContextWindow
		}
		if p := cfg.GetProviderForModel(agentCfg.Model); p != nil && config.IsCLIProviderType(p.Type) {
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
