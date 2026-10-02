package cliagent

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/stretchr/testify/require"
)

func TestCodexSteerIncludesImage(t *testing.T) {
	dir := t.TempDir()
	script := `#!/bin/sh
read -r _; echo '{"id":"1","result":{}}'
read -r _; read -r _; echo '{"id":"2","result":{"thread":{"id":"th"}}}'
read -r _; echo '{"id":"3","result":{"turn":{"id":"tu"}}}'
read -r steer
case "$steer" in *turn/steer*) ;; *) exit 2;; esac
case "$steer" in *data:image/png\;base64,aW1hZ2U=*) ;; *) exit 3;; esac
case "$steer" in *SCREEN*) ;; *) exit 4;; esac
echo '{"id":"steer-1","result":{"turnId":"tu"}}'
echo '{"method":"item/started","params":{"item":{"type":"userMessage","id":"u2","content":[{"type":"text","text":"SCREEN"},{"type":"image","url":"data:image/png;base64,aW1hZ2U="}]}}}'
echo '{"method":"turn/completed","params":{"turn":{"id":"tu","status":"completed"}}}'
cat >/dev/null
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "codex"), []byte(script), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	var sent atomic.Bool
	var confirmed []string
	model := &Model{Kind: config.TypeCodexCLI, ID: "fixture", Dir: dir}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	err := model.Run(ctx, Turn{Prompt: "start", Emit: func(event Event) error {
		if event.Type == EventUserMessage {
			confirmed = append(confirmed, event.Text)
		}
		return nil
	}, SteerInput: func() (string, []message.Attachment) {
		if sent.Swap(true) {
			return "", nil
		}
		return "SCREEN", []message.Attachment{{MimeType: "image/png", Content: []byte("image")}}
	}})
	require.NoError(t, err)
	require.Equal(t, []string{"SCREEN"}, confirmed)
}

func TestPollSteerInputCarriesImageOnlyRequest(t *testing.T) {
	ready := make(chan struct{}, 1)
	received := make(chan []message.Attachment, 1)
	image := message.Attachment{MimeType: "image/png", Content: []byte("image")}
	var sent atomic.Bool
	stop := pollSteerInput(Turn{SteerReady: ready, SteerInput: func() (string, []message.Attachment) {
		if sent.Swap(true) {
			return "", nil
		}
		return "", []message.Attachment{image}
	}}, func() bool { return true }, func(_ string, images []message.Attachment) { received <- images })
	defer stop()
	ready <- struct{}{}
	select {
	case images := <-received:
		require.Equal(t, []message.Attachment{image}, images)
	case <-time.After(time.Second):
		t.Fatal("image was left queued")
	}
}
