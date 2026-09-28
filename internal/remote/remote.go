// Package remote shares the chat a Crush window shows with the Pocket
// Agents phone app, like Claude Code's Remote Control. /remote starts a
// small HTTP server in the window's process. It listens on the machine's
// Tailscale address only and answers only devices of the same Tailscale
// user. The phone gets the chat as a stream of Items (Server-Sent Events)
// and acts on it with small JSON requests. Actions that the TUI itself
// performs (sending, stopping, switching models...) are handed to the TUI
// through Actions, so both sides stay in step exactly as if the user had
// pressed the keys; answers to prompts and questions go straight to the
// services, which the TUI already follows.
package remote

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/question"
)

// ProtocolVersion is bumped on breaking changes to the phone protocol.
const ProtocolVersion = 1

// ActionKind is something the phone asks the TUI to do.
type ActionKind int

const (
	ActSend       ActionKind = iota // send Text and Attachments (queued while busy)
	ActCancel                       // Esc: clear the queue, else stop the run
	ActInterrupt                    // Ctrl+Enter: stop and send the queue now
	ActBackground                   // Ctrl+B: move the running command to the background
	ActClearQueue                   // drop the queued prompts
	ActModel                        // switch to Provider/Model
	ActEffort                       // set the reasoning Effort
	ActThink                        // toggle thinking
	ActFast                         // toggle FAST mode
	ActYolo                         // toggle YOLO mode
	ActMode                         // switch between code and plan mode
	ActSummarize                    // /compact
)

// Action is a phone request for the TUI. The TUI must call Done exactly
// once with the outcome.
type Action struct {
	Kind        ActionKind
	SessionID   string
	Text        string
	Attachments []message.Attachment
	Provider    string
	Model       string
	Effort      string

	reply chan error
}

// Done reports the action's outcome back to the phone.
func (a *Action) Done(err error) {
	select {
	case a.reply <- err:
	default:
	}
}

// Presence is what the TUI tells the server about the window.
type Presence struct {
	// SessionID is the chat the window shows; empty on the start screen.
	SessionID string
	// Focused is true while the user looks at this terminal window.
	Focused bool
	// Plan is true in plan mode.
	Plan bool
}

// Status is what the TUI shows about the share.
type Status struct {
	Host   string
	Addr   string
	Phones []string
}

// Server is one window's share.
type Server struct {
	src  Source
	hub  *hub
	gate *gate
	ln   net.Listener
	http *http.Server
	host string

	actions chan *Action
	stopped <-chan struct{}
	cancel  context.CancelFunc
	done    chan struct{}
}

// Start shares the window. It fails when Tailscale isn't running here.
func Start(src Source) (*Server, error) {
	api := newLocalAPI(localAPISocket)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	me, err := api.self(ctx)
	cancel()
	if err != nil {
		return nil, fmt.Errorf("can't reach Tailscale on this computer: %w", err)
	}
	if !me.Online || !me.IPv4.IsValid() {
		return nil, errors.New("Tailscale is not connected on this computer")
	}
	return start(src, me, api.whois, func() (net.Listener, error) { return listenTailnet(me.IPv4) })
}

func start(src Source, me self, whois func(context.Context, string) (peer, error), listen func() (net.Listener, error)) (*Server, error) {
	ln, err := listen()
	if err != nil {
		return nil, err
	}
	port := 0
	if ap, err := netip.ParseAddrPort(ln.Addr().String()); err == nil {
		port = int(ap.Port())
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Server{
		src:     src,
		gate:    &gate{whois: whois, user: me.User},
		ln:      ln,
		host:    me.Host,
		actions: make(chan *Action),
		stopped: ctx.Done(),
		cancel:  cancel,
		done:    make(chan struct{}),
	}
	s.hub = newHub(src, me.Host, port)
	s.http = &http.Server{
		Handler:           s.routes(),
		ReadHeaderTimeout: 10 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}
	go func() {
		defer close(s.done)
		if err := s.http.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("Remote server stopped", "error", err)
		}
	}()
	go s.hub.run(ctx)
	slog.Info("Remote control on", "addr", ln.Addr().String())
	return s, nil
}

// Stop ends the share and disconnects every phone. The streams end first,
// before their requests are cancelled, so each phone gets the stopped event.
func (s *Server) Stop() {
	s.hub.closeAll()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = s.http.Shutdown(ctx)
	s.cancel()
	<-s.done
}

// Actions delivers the phone's requests for the TUI.
func (s *Server) Actions() <-chan *Action { return s.actions }

// PhonesChanged fires when a phone connects or disconnects.
func (s *Server) PhonesChanged() <-chan struct{} { return s.hub.changes }

// Stopped is closed once the share stops.
func (s *Server) Stopped() <-chan struct{} { return s.stopped }

// SetPresence updates what the phone knows about the window.
func (s *Server) SetPresence(p Presence) { s.hub.setPresence(p) }

// Status describes the share for the TUI.
func (s *Server) Status() Status {
	return Status{Host: s.host, Addr: s.ln.Addr().String(), Phones: s.hub.phones()}
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/share", s.handleShare)
	mux.HandleFunc("GET /v1/events", s.handleEvents)
	mux.HandleFunc("GET /v1/steps/{id}", s.handleStep)
	mux.HandleFunc("GET /v1/media/{msg}/{idx}", s.handleMedia)
	mux.HandleFunc("POST /v1/send", s.handleSend)
	mux.HandleFunc("POST /v1/cancel", s.simple(ActCancel))
	mux.HandleFunc("POST /v1/interrupt", s.simple(ActInterrupt))
	mux.HandleFunc("POST /v1/background", s.simple(ActBackground))
	mux.HandleFunc("POST /v1/queue/clear", s.simple(ActClearQueue))
	mux.HandleFunc("POST /v1/model", s.handleModel)
	mux.HandleFunc("POST /v1/effort", s.handleEffort)
	mux.HandleFunc("POST /v1/command", s.handleCommand)
	mux.HandleFunc("POST /v1/permission", s.handlePermission)
	mux.HandleFunc("POST /v1/question", s.handleQuestion)
	mux.HandleFunc("POST /v1/rename", s.handleRename)
	mux.HandleFunc("POST /v1/tasks/{id}/stop", s.handleStopTask)
	mux.HandleFunc("POST /v1/processes/{pid}/kill", s.handleKillProcess)
	return s.guard(mux)
}

type peerKey struct{}

// guard lets only the owner's tailnet devices in.
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, err := s.gate.check(r.Context(), r.RemoteAddr)
		if err != nil {
			slog.Warn("Remote request refused", "from", r.RemoteAddr, "error", err)
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), peerKey{}, p)))
	})
}

func (s *Server) handleShare(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.hub.share())
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	p, _ := r.Context().Value(peerKey{}).(peer)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	// A lite stream (?lite=1) carries only what the phone needs to list the
	// share and alert; the phone uses it for chats it isn't showing.
	c, snap := s.hub.register(p.Name, r.URL.Query().Get("lite") == "1")
	defer s.hub.unregister(c)
	if snap == nil {
		return
	}
	if _, err := w.Write(sse(snap)); err != nil {
		return
	}
	flusher.Flush()

	ping := time.NewTicker(15 * time.Second)
	defer ping.Stop()
	for {
		select {
		case b, ok := <-c.ch:
			if !ok {
				return
			}
			if _, err := w.Write(sse(b)); err != nil {
				return
			}
			flusher.Flush()
		case <-ping.C:
			if _, err := w.Write([]byte(": ping\n\n")); err != nil {
				return
			}
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func sse(b []byte) []byte {
	out := make([]byte, 0, len(b)+8)
	out = append(out, "data: "...)
	out = append(out, b...)
	return append(out, '\n', '\n')
}

func (s *Server) handleStep(w http.ResponseWriter, r *http.Request) {
	d, ok := s.hub.stepDetail(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, errors.New("no such step"))
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) handleMedia(w http.ResponseWriter, r *http.Request) {
	mime, data, ok := s.hub.media(r.PathValue("msg"), r.PathValue("idx"))
	if !ok {
		writeError(w, http.StatusNotFound, errors.New("no such file"))
		return
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Cache-Control", "private, max-age=86400")
	_, _ = w.Write(data)
}

// request is the body every phone action carries.
type request struct {
	// Session is the chat the phone is looking at. Actions for a chat the
	// window no longer shows are refused.
	Session     string            `json:"session"`
	Text        string            `json:"text,omitempty"`
	Attachments []upload          `json:"attachments,omitempty"`
	Provider    string            `json:"provider,omitempty"`
	Model       string            `json:"model,omitempty"`
	Effort      string            `json:"effort,omitempty"`
	Command     string            `json:"command,omitempty"`
	ID          string            `json:"id,omitempty"`
	Answer      string            `json:"answer,omitempty"`
	Answers     []question.Answer `json:"answers,omitempty"`
	Cancel      bool              `json:"cancel,omitempty"`
	Title       string            `json:"title,omitempty"`
}

type upload struct {
	Name string `json:"name"`
	Mime string `json:"mime"`
	Data string `json:"data"` // base64
}

const maxBody = 64 << 20

// readRequest decodes the body and checks it is about the current chat.
func (s *Server) readRequest(w http.ResponseWriter, r *http.Request) (request, bool) {
	var req request
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody)).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, fmt.Errorf("bad request: %w", err))
		return req, false
	}
	current := s.hub.currentSession()
	if current == "" {
		writeError(w, http.StatusConflict, errors.New("no chat is open in this Crush window"))
		return req, false
	}
	if req.Session != current {
		writeError(w, http.StatusConflict, errors.New("the Crush window switched to another chat"))
		return req, false
	}
	return req, true
}

// dispatch hands an action to the TUI and waits for its outcome.
func (s *Server) dispatch(w http.ResponseWriter, r *http.Request, a *Action) {
	a.reply = make(chan error, 1)
	select {
	case s.actions <- a:
	case <-time.After(5 * time.Second):
		writeError(w, http.StatusServiceUnavailable, errors.New("the Crush window is not responding"))
		return
	case <-r.Context().Done():
		return
	}
	select {
	case err := <-a.reply:
		if err != nil {
			writeError(w, http.StatusConflict, err)
			return
		}
		s.hub.poke()
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	case <-time.After(30 * time.Second):
		writeError(w, http.StatusGatewayTimeout, errors.New("the Crush window did not answer"))
	case <-r.Context().Done():
	}
}

func (s *Server) simple(kind ActionKind) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		req, ok := s.readRequest(w, r)
		if !ok {
			return
		}
		s.dispatch(w, r, &Action{Kind: kind, SessionID: req.Session})
	}
}

func (s *Server) handleSend(w http.ResponseWriter, r *http.Request) {
	req, ok := s.readRequest(w, r)
	if !ok {
		return
	}
	atts, err := saveUploads(req.Attachments)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(req.Text) == "" && len(atts) == 0 {
		writeError(w, http.StatusBadRequest, errors.New("nothing to send"))
		return
	}
	s.dispatch(w, r, &Action{Kind: ActSend, SessionID: req.Session, Text: req.Text, Attachments: atts})
}

// saveUploads keeps a copy of every phone upload so agents can also open
// it by path.
func saveUploads(ups []upload) ([]message.Attachment, error) {
	if len(ups) == 0 {
		return nil, nil
	}
	dir := uploadDir()
	var out []message.Attachment
	for i, u := range ups {
		data, err := base64.StdEncoding.DecodeString(u.Data)
		if err != nil {
			return nil, fmt.Errorf("attachment %d: %w", i+1, err)
		}
		name := filepath.Base(strings.TrimSpace(u.Name))
		if name == "" || name == "." || name == "/" {
			name = "attachment"
		}
		// A folder per upload keeps the file's own name, which the chat shows.
		folder := filepath.Join(dir, time.Now().Format("20060102-150405")+"-"+strconv.Itoa(i))
		if err := os.MkdirAll(folder, 0o700); err != nil {
			return nil, err
		}
		path := filepath.Join(folder, name)
		if err := os.WriteFile(path, data, 0o600); err != nil {
			return nil, err
		}
		mime := u.Mime
		if mime == "" {
			mime = http.DetectContentType(data)
		}
		out = append(out, message.Attachment{FilePath: path, FileName: name, MimeType: mime, Content: data})
	}
	return out, nil
}

func uploadDir() string {
	if d, err := os.UserCacheDir(); err == nil {
		return filepath.Join(d, "crush", "uploads")
	}
	return filepath.Join(os.TempDir(), "crush-uploads")
}

func (s *Server) handleModel(w http.ResponseWriter, r *http.Request) {
	req, ok := s.readRequest(w, r)
	if !ok {
		return
	}
	if req.Provider == "" || req.Model == "" {
		writeError(w, http.StatusBadRequest, errors.New("provider and model are required"))
		return
	}
	s.dispatch(w, r, &Action{Kind: ActModel, SessionID: req.Session, Provider: req.Provider, Model: req.Model})
}

func (s *Server) handleEffort(w http.ResponseWriter, r *http.Request) {
	req, ok := s.readRequest(w, r)
	if !ok {
		return
	}
	s.dispatch(w, r, &Action{Kind: ActEffort, SessionID: req.Session, Effort: req.Effort})
}

func (s *Server) handleCommand(w http.ResponseWriter, r *http.Request) {
	req, ok := s.readRequest(w, r)
	if !ok {
		return
	}
	kinds := map[string]ActionKind{
		"summarize": ActSummarize,
		"think":     ActThink,
		"fast":      ActFast,
		"yolo":      ActYolo,
		"mode":      ActMode,
	}
	kind, known := kinds[req.Command]
	if !known {
		writeError(w, http.StatusBadRequest, fmt.Errorf("unknown command %q", req.Command))
		return
	}
	s.dispatch(w, r, &Action{Kind: kind, SessionID: req.Session})
}

func (s *Server) handlePermission(w http.ResponseWriter, r *http.Request) {
	req, ok := s.readRequest(w, r)
	if !ok {
		return
	}
	perm, pending := s.src.PendingPermission()
	if !pending || perm.ID != req.ID {
		writeError(w, http.StatusConflict, errors.New("that request was already answered"))
		return
	}
	var resolved bool
	switch req.Answer {
	case "allow":
		resolved = s.src.PermissionGrant(perm)
	case "session":
		resolved = s.src.PermissionGrantPersistent(perm)
	case "deny":
		resolved = s.src.PermissionDeny(perm)
	default:
		writeError(w, http.StatusBadRequest, fmt.Errorf("unknown answer %q", req.Answer))
		return
	}
	if !resolved {
		writeError(w, http.StatusConflict, errors.New("that request was already answered"))
		return
	}
	s.hub.poke()
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleQuestion(w http.ResponseWriter, r *http.Request) {
	req, ok := s.readRequest(w, r)
	if !ok {
		return
	}
	q, pending := s.src.PendingQuestion()
	if !pending || q.ID != req.ID {
		writeError(w, http.StatusConflict, errors.New("those questions were already answered"))
		return
	}
	var resolved bool
	if req.Cancel {
		resolved = s.src.QuestionCancel()
	} else {
		resolved = s.src.QuestionAnswer(req.Answers)
	}
	if !resolved {
		writeError(w, http.StatusConflict, errors.New("those questions were already answered"))
		return
	}
	s.hub.poke()
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleRename(w http.ResponseWriter, r *http.Request) {
	req, ok := s.readRequest(w, r)
	if !ok {
		return
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		writeError(w, http.StatusBadRequest, errors.New("the name is empty"))
		return
	}
	sess, err := s.src.GetSession(r.Context(), req.Session)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	sess.Title = title
	if _, err := s.src.SaveSession(r.Context(), sess); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.hub.poke()
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleStopTask(w http.ResponseWriter, r *http.Request) {
	req, ok := s.readRequest(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	owned := false
	for _, t := range s.src.Tasks(req.Session) {
		owned = owned || t.ID == id
	}
	if !owned {
		writeError(w, http.StatusNotFound, errors.New("no such sub-agent in this chat"))
		return
	}
	if err := s.src.StopTask(id); err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	s.hub.poke()
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleKillProcess(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.readRequest(w, r); !ok {
		return
	}
	pid, err := strconv.Atoi(r.PathValue("pid"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := s.src.KillProcess(pid); err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	s.hub.refreshProcesses()
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}

// client is one connected phone.
type client struct {
	name string
	lite bool
	ch   chan []byte
	once sync.Once
}

func (c *client) close() { c.once.Do(func() { close(c.ch) }) }
