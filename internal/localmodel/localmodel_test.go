package localmodel

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func weight(t *testing.T, path string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
	require.NoError(t, os.WriteFile(path, []byte("GGUFfixture"), 0600))
}

func TestDiscoveryKeepsCompleteWeightsAndHidesPaths(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"A.gguf", "mmproj-A.gguf", "ggml-vocab-test.gguf", "Split-00001-of-00002.gguf", "Split-00002-of-00002.gguf", "Missing-00001-of-00002.gguf", ".unsloth/studio/Secret.gguf"} {
		weight(t, filepath.Join(dir, name))
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Invalid.gguf"), []byte("HTML"), 0600))
	require.NoError(t, os.Link(filepath.Join(dir, "A.gguf"), filepath.Join(dir, "Duplicate.gguf")))
	require.NoError(t, os.Symlink(filepath.Join(dir, ".unsloth/studio/Secret.gguf"), filepath.Join(dir, "VaultAlias.gguf")))
	models := Discover([]string{dir, dir})
	require.Len(t, models, 2)
	require.Equal(t, "A.gguf", models[0].Name)
	require.Equal(t, int64(22), models[1].Bytes)
	encoded, err := json.Marshal(models)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), dir)
	require.Equal(t, models, Discover([]string{dir}))
}

// This server accepts the real launch flags but never opens weights or a GPU.
// It also refuses startup if sensitive inherited flags reach the child.
const fakeServer = `#!/usr/bin/python3
import argparse, json, os
from http.server import BaseHTTPRequestHandler, HTTPServer
p=argparse.ArgumentParser(); p.add_argument('--port',type=int); p.add_argument('--model'); p.add_argument('--alias')
a,flags=p.parse_known_args()
assert '--no-agent' in flags
assert 'LLAMA_ARG_AGENT' not in os.environ and 'LOCAL_TEST_SECRET' not in os.environ
class Handler(BaseHTTPRequestHandler):
 def log_message(self,*args): pass
 def do_GET(self):
  self.send_response(503 if 'Slow' in a.model else 200); self.end_headers()
 def do_POST(self):
  body=json.loads(self.rfile.read(int(self.headers['Content-Length'])))
  self.send_response(200); self.send_header('Content-Type','application/json'); self.end_headers()
  self.wfile.write(json.dumps({'model':a.alias,'answer':'fixture'}).encode())
HTTPServer(('127.0.0.1',a.port),Handler).serve_forever()
`

func testManager(t *testing.T) (*Manager, []Model) {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{"A.gguf", "B.gguf", "Slow.gguf"} {
		weight(t, filepath.Join(dir, name))
	}
	server := filepath.Join(dir, "llama-server")
	require.NoError(t, os.WriteFile(server, []byte(fakeServer), 0700))
	t.Setenv("CRUSH_LLAMA_SERVER", server)
	t.Setenv("CRUSH_LOCAL_MODEL_DIRS", dir)
	t.Setenv("LLAMA_ARG_AGENT", "1")
	t.Setenv("LOCAL_TEST_SECRET", "must-not-reach-child")
	m := &Manager{}
	t.Cleanup(m.Close)
	return m, Discover([]string{dir})
}

func TestLoadSwitchRelayAndUnload(t *testing.T) {
	m, models := testManager(t)
	endpoint, key, err := m.Endpoint()
	require.NoError(t, err)
	call := func(model, token string) (int, string) {
		t.Helper()
		req, err := http.NewRequest("POST", endpoint+"/chat/completions", bytes.NewBufferString(`{"model":"`+model+`"}`))
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		return resp.StatusCode, string(body)
	}
	code, _ := call(models[0].ID, key)
	require.Equal(t, 409, code)
	require.NoError(t, m.Load(models[0].ID))
	require.Eventually(t, func() bool { return m.Status().State == "ready" }, 5*time.Second, 20*time.Millisecond)
	code, body := call(models[0].ID, key)
	require.Equal(t, 200, code)
	require.Contains(t, body, "fixture")
	code, _ = call(models[0].ID, "wrong")
	require.Equal(t, 401, code)
	code, _ = call(models[1].ID, key)
	require.Equal(t, 409, code)
	require.Error(t, m.Load("/arbitrary/path.gguf"))
	require.Equal(t, "ready", m.Status().State)
	old := m.cmd
	require.NoError(t, m.Load(models[1].ID))
	require.NotNil(t, old.ProcessState, "old process must exit before replacement")
	require.Eventually(t, func() bool { return m.Status().State == "ready" }, 5*time.Second, 20*time.Millisecond)
	code, _ = call(models[0].ID, key)
	require.Equal(t, 409, code)
	require.NoError(t, m.Unload())
	require.Equal(t, "unloaded", m.Status().State)
	code, _ = call(models[1].ID, key)
	require.Equal(t, 409, code)
	require.NoError(t, m.Unload())
}

func TestUnloadDuringLoadingAndServerFailure(t *testing.T) {
	m, models := testManager(t)
	require.NoError(t, m.Load(models[2].ID))
	require.Equal(t, "loading", m.Status().State)
	require.NoError(t, m.Unload())
	require.Equal(t, "unloaded", m.Status().State)
	server := os.Getenv("CRUSH_LLAMA_SERVER")
	require.NoError(t, os.WriteFile(server, []byte("#!/bin/sh\necho 'fixture: unsupported architecture' >&2\nexit 1\n"), 0700))
	require.NoError(t, m.Load(models[0].ID))
	require.Eventually(t, func() bool { return m.Status().State == "error" }, 3*time.Second, 20*time.Millisecond)
	require.True(t, strings.Contains(m.Status().Error, "unsupported architecture"))
	require.NoError(t, m.Unload())
}
