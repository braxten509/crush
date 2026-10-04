package remote

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// The launcher lets the owner's phone open a new Crush window on this
// computer, in the home folder, already shared with the phone. It runs as
// an always-on service (`crush remote launcher`) on the tailnet address
// only, answers only the owner's own Tailscale devices, and starts a window
// only for the password set with `crush remote password`, which is kept as
// a bcrypt hash. Wrong passwords are slowed down: after a few in a row the
// launcher refuses every try for a while.

// LauncherPort sits just below the shares' range, so the phone finds it the
// same way it finds the windows.
const LauncherPort = firstPort - 1

// LaunchEnv carries the phone's launch id into the window it asked for,
// which then shares itself and reports the id back.
const LaunchEnv = "CRUSH_REMOTE_LAUNCH"

const (
	minPasswordLen = 8
	maxWrongTries  = 5
	lockout        = time.Minute
)

var launchID atomic.Value

// SetLaunchID records the launch id this window was started with.
func SetLaunchID(id string) { launchID.Store(id) }

// LaunchID is the launch id this window was started with, if any.
func LaunchID() string {
	id, _ := launchID.Load().(string)
	return id
}

// SetPassword stores the launcher password's hash in file (owner-only).
func SetPassword(file, password string) error {
	if len(password) < minPasswordLen {
		return fmt.Errorf("the password needs at least %d characters", minPasswordLen)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return err
	}
	tmp := file + ".tmp"
	if err := os.WriteFile(tmp, hash, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, file)
}

// LaunchCommand builds the command that opens the new window; id goes to it
// in LaunchEnv. The default opens Konsole in the home folder running this
// Crush; CRUSH_LAUNCH_COMMAND (a shell command line) replaces it.
type LaunchCommand func(id string) (*exec.Cmd, error)

// DefaultLaunchCommand opens a Konsole window running crush in home.
func DefaultLaunchCommand(crush, home string) LaunchCommand {
	return func(id string) (*exec.Cmd, error) {
		var cmd *exec.Cmd
		if line := strings.TrimSpace(os.Getenv("CRUSH_LAUNCH_COMMAND")); line != "" {
			cmd = exec.Command("sh", "-c", line)
		} else {
			konsole, err := exec.LookPath("konsole")
			if err != nil {
				return nil, errors.New("Konsole isn't installed on this computer")
			}
			// --separate: a new Konsole process, so the window gets this
			// environment rather than the running Konsole's.
			cmd = exec.Command(konsole, "--separate", "--workdir", home, "-e", crush)
		}
		cmd.Dir = home
		cmd.Env = append(os.Environ(), LaunchEnv+"="+id, "CRUSH_EXE="+crush)
		return cmd, nil
	}
}

// launcher answers the phone.
type launcher struct {
	host         string
	passwordFile string
	command      LaunchCommand

	mu     sync.Mutex
	wrong  int
	lockAt time.Time
	now    func() time.Time
}

func (l *launcher) routes(g *gate) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/launcher", l.handleInfo)
	mux.HandleFunc("POST /v1/launch", l.handleLaunch)
	return guardWith(g, mux)
}

type launcherInfo struct {
	Version     int    `json:"version"`
	Host        string `json:"host"`
	PasswordSet bool   `json:"password_set"`
}

func (l *launcher) handleInfo(w http.ResponseWriter, _ *http.Request) {
	_, err := os.Stat(l.passwordFile)
	writeJSON(w, http.StatusOK, launcherInfo{Version: ProtocolVersion, Host: l.host, PasswordSet: err == nil})
}

// check says whether password is the one set, counting wrong tries.
func (l *launcher) check(password string) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.lockAt.IsZero() && l.now().Sub(l.lockAt) < lockout {
		return false, errors.New("Too many wrong passwords. Try again in a minute.")
	}
	hash, err := os.ReadFile(l.passwordFile)
	if err != nil {
		return false, errors.New("No password is set yet. On the PC, run: crush remote password")
	}
	if bcrypt.CompareHashAndPassword(hash, []byte(password)) != nil {
		l.wrong++
		if l.wrong >= maxWrongTries {
			l.wrong = 0
			l.lockAt = l.now()
		}
		return false, nil
	}
	l.wrong = 0
	l.lockAt = time.Time{}
	return true, nil
}

func (l *launcher) handleLaunch(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, errors.New("bad request"))
		return
	}
	ok, err := l.check(req.Password)
	if err != nil {
		writeError(w, http.StatusTooManyRequests, err)
		return
	}
	if !ok {
		p, _ := r.Context().Value(peerKey{}).(peer)
		slog.Warn("Launcher refused a wrong password", "from", p.Name)
		writeError(w, http.StatusUnauthorized, errors.New("Wrong password."))
		return
	}
	id := newLaunchID()
	cmd, err := l.command(id)
	if err == nil {
		err = cmd.Start()
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("Couldn't open a Crush window: %w", err))
		return
	}
	go func() { _ = cmd.Wait() }()
	p, _ := r.Context().Value(peerKey{}).(peer)
	slog.Info("Launcher opened a Crush window", "for", p.Name, "launch", id)
	writeJSON(w, http.StatusOK, map[string]string{"launch": id})
}

func newLaunchID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// RunLauncher serves the launcher until ctx ends. It waits for Tailscale to
// come up on this computer, so it can start with the session.
func RunLauncher(ctx context.Context, passwordFile string, command LaunchCommand) error {
	api := newLocalAPI(localAPISocket)
	var me self
	for {
		c, cancel := context.WithTimeout(ctx, 5*time.Second)
		got, err := api.self(c)
		cancel()
		if err == nil && got.Online && got.IPv4.IsValid() {
			me = got
			break
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(10 * time.Second):
		}
	}
	ln, err := net.Listen("tcp", netip.AddrPortFrom(me.IPv4, LauncherPort).String())
	if err != nil {
		return err
	}
	return serveLauncher(ctx, ln, &gate{whois: api.whois, user: me.User},
		&launcher{host: me.Host, passwordFile: passwordFile, command: command, now: time.Now})
}

func serveLauncher(ctx context.Context, ln net.Listener, g *gate, l *launcher) error {
	srv := &http.Server{Handler: l.routes(g), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		c, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(c)
	}()
	slog.Info("Launcher on", "addr", ln.Addr().String())
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
