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

	"github.com/charmbracelet/crush/internal/agent"
	"github.com/charmbracelet/crush/internal/agent/notify"
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
			// A turn that ends while work still runs (a progress check-in,
			// say) is not the end: the phone would beep for nothing.
			if h.src.HasRunningWork(cur) {
				return true
			}
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
