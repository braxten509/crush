package localmodel

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Status struct {
	Models  []Model `json:"models"`
	ModelID string  `json:"model_id"`
	State   string  `json:"state"`
	Error   string  `json:"error,omitempty"`
}

type Manager struct {
	op       sync.Mutex
	mu       sync.Mutex
	state    string
	id       string
	err      string
	cmd      *exec.Cmd
	done     chan struct{}
	cancel   context.CancelFunc
	target   string
	endpoint string
	key      string
	server   *http.Server
}

var Default = &Manager{}

func (m *Manager) Status() Status {
	models := Discover(Roots())
	m.mu.Lock()
	defer m.mu.Unlock()
	state := m.state
	if state == "" {
		state = "unloaded"
	}
	return Status{Models: models, ModelID: m.id, State: state, Error: m.err}
}

// Endpoint is a process-owned loopback relay. It refuses a different model ID
// rather than silently answering one chat with another chat's loaded weights.
func (m *Manager) Endpoint() (string, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.endpoint != "" {
		return m.endpoint, m.key, nil
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", "", err
	}
	m.key = fmt.Sprintf("%x", rand.Text())
	m.endpoint = "http://" + ln.Addr().String() + "/v1"
	m.server = &http.Server{Handler: http.HandlerFunc(m.serve), ReadHeaderTimeout: 10 * time.Second}
	go m.server.Serve(ln)
	return m.endpoint, m.key, nil
}

func (m *Manager) serve(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	state, id, target, key := m.state, m.id, m.target, m.key
	m.mu.Unlock()
	fail := func(code int, text string) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"message": text, "type": "local_model_error"}})
	}
	if r.Header.Get("Authorization") != "Bearer "+key {
		fail(401, "unauthorized")
		return
	}
	if state != "ready" {
		fail(409, "Load the local model in Crush's provider list before sending a message")
		return
	}
	if r.Method != http.MethodPost || (r.URL.Path != "/v1/chat/completions" && r.URL.Path != "/v1/completions" && r.URL.Path != "/v1/embeddings") {
		fail(404, "route not available")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 32<<20))
	if err != nil {
		fail(400, "request body is too large or unreadable")
		return
	}
	var request struct {
		Model string `json:"model"`
	}
	if json.Unmarshal(body, &request) != nil || request.Model != id {
		fail(409, "This local model is not loaded; select it and click Load in the provider list")
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	r.ContentLength = int64(len(body))
	upstream, _ := url.Parse(target)
	proxy := httputil.NewSingleHostReverseProxy(upstream)
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, _ error) {
		fail(502, "The local model server stopped; load the model again")
	}
	proxy.ServeHTTP(w, r)
}

// Load returns immediately; the UI polls Status until the health check passes.
// Only explicit user actions call this: discovery and selection load nothing.
func (m *Manager) Load(id string) error {
	var selected *Model
	for _, model := range Discover(Roots()) {
		if model.ID == id {
			selected = &model
			break
		}
	}
	if selected == nil {
		return fmt.Errorf("downloaded model was not found; reopen the provider list")
	}
	server, err := ServerExecutable()
	if err != nil {
		return err
	}
	m.op.Lock()
	defer m.op.Unlock()
	m.mu.Lock()
	same := m.id == id && (m.state == "ready" || m.state == "loading")
	m.mu.Unlock()
	if same {
		return nil
	}
	if err := m.stop(); err != nil {
		return err
	}
	port, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	address := port.Addr().(*net.TCPAddr)
	number := address.Port
	port.Close()
	cmd := exec.Command(server, "--model", selected.Path, "--host", "127.0.0.1", "--port", fmt.Sprint(number), "--alias", id,
		"--ctx-size", fmt.Sprint(ContextSize), "--parallel", "1", "--gpu-layers", "auto", "--flash-attn", "auto", "--jinja", "--log-disable", "--no-agent")
	// Avoid inheriting LLAMA_ARG_* (especially agent tools) or provider secrets.
	home, _ := os.UserHomeDir()
	cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + home, "LANG=C.UTF-8", "LD_LIBRARY_PATH=" + filepath.Dir(server)}
	prepareProcess(cmd)
	log := &tailBuffer{}
	cmd.Stdout = log
	cmd.Stderr = log
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("could not start llama-server: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	done := make(chan struct{})
	m.mu.Lock()
	m.id, m.state, m.err, m.cmd, m.done, m.cancel, m.target = id, "loading", "", cmd, done, cancel, fmt.Sprintf("http://127.0.0.1:%d", number)
	m.mu.Unlock()
	go func() {
		err := cmd.Wait()
		m.mu.Lock()
		if m.cmd == cmd && m.state != "stopping" {
			m.state = "error"
			m.err = fmt.Sprintf("llama-server exited (%v): %s", err, log.String())
		}
		close(done)
		m.mu.Unlock()
	}()
	go m.waitReady(ctx, cmd, done, number)
	return nil
}

func (m *Manager) waitReady(ctx context.Context, cmd *exec.Cmd, done <-chan struct{}, port int) {
	client := &http.Client{Timeout: time.Second, Transport: &http.Transport{Proxy: nil}}
	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ctx.Done():
			m.mu.Lock()
			if m.cmd == cmd && m.state == "loading" {
				m.state = "error"
				m.err = "Model loading timed out; try a smaller model or a smaller context"
				cmd.Process.Kill()
			}
			m.mu.Unlock()
			return
		case <-ticker.C:
			resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/health", port))
			if err != nil {
				continue
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode != 200 {
				continue
			}
			m.mu.Lock()
			if m.cmd == cmd && m.state == "loading" {
				m.state = "ready"
			}
			m.mu.Unlock()
			return
		}
	}
}

func (m *Manager) Unload() error { m.op.Lock(); defer m.op.Unlock(); return m.stop() }

// stop only signals the child we own. It never uses pkill, a stored PID, or an
// external server's unload endpoint, and waits for exit before another load.
func (m *Manager) stop() error {
	m.mu.Lock()
	cmd, done := m.cmd, m.done
	m.state = "stopping"
	if m.cancel != nil {
		m.cancel()
	}
	m.mu.Unlock()
	if cmd != nil {
		_ = cmd.Process.Kill()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			return fmt.Errorf("the previous local model process has not stopped")
		}
	}
	m.mu.Lock()
	m.state, m.id, m.err, m.target, m.cmd, m.done = "unloaded", "", "", "", nil, nil
	m.mu.Unlock()
	return nil
}

func (m *Manager) Close() {
	_ = m.Unload()
	m.mu.Lock()
	if m.server != nil {
		m.server.Close()
	}
	m.mu.Unlock()
}

type tailBuffer struct {
	mu   sync.Mutex
	data []byte
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.data = append(b.data, p...)
	if len(b.data) > 2048 {
		b.data = b.data[len(b.data)-2048:]
	}
	return len(p), nil
}
func (b *tailBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.TrimSpace(string(b.data))
}
