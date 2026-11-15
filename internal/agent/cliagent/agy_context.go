package cliagent

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/jsonfile"
)

// AGY's print mode never says how large the model's context window is. Its
// ACP server does, in a usage_update, as soon as a session has its model:
// no prompt is sent and no model request is made. Each ask leaves an empty
// conversation in AGY's history, so a model's window is saved and asked
// again only once it is agyWindowAge old.

// agyWindow is one model's saved context window.
type agyWindow struct {
	Size int64     `json:"size"`
	At   time.Time `json:"at"`
}

// agyWindowAge is how long a saved window is used before AGY is asked again.
const agyWindowAge = 7 * 24 * time.Hour

// agyWindowTimeout bounds one ask; agyWindowSettle is how long the answer
// may trail AGY's reply to the model change.
const (
	agyWindowTimeout = 20 * time.Second
	agyWindowSettle  = 3 * time.Second
)

// agyAskWindow replaces [askAGYWindow] in tests. A test that leaves it
// unset never starts a real tool.
var agyAskWindow func(ctx context.Context, model string) (int64, error)

// agyWindowAsking holds the models being asked about, so turns that start
// together share one ask.
var agyWindowAsking sync.Map // model ID -> struct{}

func agyWindowFile() string {
	return filepath.Join(config.GlobalCacheDir(), "agy-context.json")
}

// agyContextBudget is the context meter's limit for a window. AGY does not
// report where below the window it compacts, so the limit is estimated.
func agyContextBudget(window int64) Event {
	return Event{Type: EventContextBudget, ContextLimit: max(0, window), ContextEstimated: true}
}

// agyContextWindow returns the model's saved context window, zero when
// there is none. When that is missing or old it asks AGY in the background
// and delivers a window that differs on asked.
func agyContextWindow(ctx context.Context, model string) (size int64, asked <-chan int64) {
	saved := jsonfile.Read[map[string]agyWindow](agyWindowFile())[model]
	if saved.Size > 0 && time.Since(saved.At) < agyWindowAge {
		return saved.Size, nil
	}
	ask := agyAskWindow
	if ask == nil {
		if testing.Testing() {
			return saved.Size, nil
		}
		ask = askAGYWindow
	}
	if _, busy := agyWindowAsking.LoadOrStore(model, struct{}{}); busy {
		return saved.Size, nil
	}
	found := make(chan int64, 1)
	go func() {
		defer agyWindowAsking.Delete(model)
		window, err := ask(context.WithoutCancel(ctx), model)
		if err != nil || window <= 0 {
			return
		}
		_ = jsonfile.Update(agyWindowFile(), func(windows *map[string]agyWindow) bool {
			if *windows == nil {
				*windows = map[string]agyWindow{}
			}
			(*windows)[model] = agyWindow{Size: window, At: time.Now()}
			return true
		})
		if window != saved.Size {
			found <- window
		}
	}()
	return saved.Size, found
}

// askAGYWindow starts a session on AGY's ACP server, gives it the model and
// reads the context window from the usage_update that follows.
func askAGYWindow(ctx context.Context, model string) (int64, error) {
	ctx, cancel := context.WithTimeout(ctx, agyWindowTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, agyBin(), "acp")
	cmd.Dir = os.TempDir()
	ownGroup(cmd)
	cmd.Cancel = func() error {
		(&proc{cmd: cmd}).kill()
		return nil
	}
	cmd.WaitDelay = time.Second
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return 0, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return 0, err
	}
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	defer func() {
		_ = stdin.Close()
		cancel()
		_ = cmd.Wait()
	}()
	send := func(id int, method string, params map[string]any) error {
		data, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
		if err != nil {
			return err
		}
		_, err = stdin.Write(append(data, '\n'))
		return err
	}
	if err := send(1, "initialize", map[string]any{"protocolVersion": 1, "clientCapabilities": map[string]any{}}); err != nil {
		return 0, err
	}

	lines := make(chan []byte)
	go func() {
		defer close(lines)
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 64<<10), 16<<20)
		for scanner.Scan() {
			select {
			case lines <- append([]byte(nil), scanner.Bytes()...):
			case <-ctx.Done():
				return
			}
		}
	}()

	// settle runs from AGY's reply to the model change: only a
	// usage_update after it is the chosen model's, since a session starts
	// on AGY's default model.
	var settle <-chan time.Time
	for {
		var raw []byte
		select {
		case l, ok := <-lines:
			if !ok {
				return 0, errors.New("agy acp ended before it reported a context window")
			}
			raw = l
		case <-settle:
			return 0, errors.New("agy acp reported no context window")
		case <-ctx.Done():
			return 0, ctx.Err()
		}
		var msg struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
			Result struct {
				SessionID string `json:"sessionId"`
			} `json:"result"`
			Params struct {
				Update struct {
					SessionUpdate string `json:"sessionUpdate"`
					Size          int64  `json:"size"`
				} `json:"update"`
			} `json:"params"`
		}
		if json.Unmarshal(raw, &msg) != nil {
			continue
		}
		if msg.Method != "" {
			if u := msg.Params.Update; settle != nil && u.SessionUpdate == "usage_update" && u.Size > 0 {
				return u.Size, nil
			}
			continue
		}
		if msg.Error != nil {
			return 0, fmt.Errorf("agy acp: %s", msg.Error.Message)
		}
		switch string(msg.ID) {
		case "1":
			err = send(2, "session/new", map[string]any{"cwd": cmd.Dir, "mcpServers": []any{}})
		case "2":
			err = send(3, "session/set_model", map[string]any{"sessionId": msg.Result.SessionID, "modelId": model})
		case "3":
			settle = time.After(agyWindowSettle)
		}
		if err != nil {
			return 0, err
		}
	}
}
