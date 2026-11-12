package remote

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/crush/internal/message"
	"github.com/stretchr/testify/require"
)

var tinyPNG = []byte("\x89PNG\r\n\x1a\n picture")

// TestReplyPictures: a reply lists the picture files its Markdown names, in
// the order of every picture mark, and /v1/media serves only those whose
// content is a picture.
func TestReplyPictures(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	plan := filepath.Join(dir, "plan one.png")
	notes := filepath.Join(dir, "notes.png")
	require.NoError(t, os.WriteFile(plan, tinyPNG, 0o600))
	require.NoError(t, os.WriteFile(notes, []byte("just words, whatever the name says"), 0o600))

	text := "Here it is: ![the plan](<" + plan + "> \"Plan\") and ![web](https://example.com/a.png)\n" +
		"```\n![in a fence](" + plan + ")\n```\n" +
		"Not `![in code](" + plan + ")` but ![words](" + notes + ") and ![gone](missing.png)"
	require.Equal(t, []string{plan, "https://example.com/a.png", notes, "missing.png"}, pictureTargets(text))

	env := newTestEnv(t, phoneAddr)
	env.src.setChat("s1", "Chat", assistantMsg("a1", message.TextContent{Text: text}, message.Finish{Reason: message.FinishReasonEndTurn}))
	env.srv.SetPresence(Presence{SessionID: "s1"})

	snap := env.stream().next(t)
	var items []Item
	require.NoError(t, json.Unmarshal(snap["items"], &items))
	require.Len(t, items, 1)
	// The web picture is no file; the others are, whether or not they can be read.
	require.Equal(t, []Picture{
		{ID: "a1/p0", Target: plan},
		{ID: "a1/p2", Target: notes},
		{ID: "a1/p3", Target: "missing.png"},
	}, items[0].Pictures)

	get := func(id string) (int, []byte) {
		resp, err := http.Get(env.url + "/v1/media/" + id)
		require.NoError(t, err)
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, body
	}
	code, body := get("a1/p0")
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, tinyPNG, body)
	for _, id := range []string{"a1/p1", "a1/p2", "a1/p3", "a1/p9", "a1/p-1"} {
		code, _ := get(id)
		require.Equal(t, http.StatusNotFound, code, id)
	}

	require.Equal(t, "/work/out/a.png", pictureFile("out/a.png", "/work"))
	require.Equal(t, "/tmp/a b.png", pictureFile("file:///tmp/a%20b.png", "/work"))
	require.Empty(t, pictureFile("data:image/png;base64,AAAA", "/work"))
	require.Empty(t, pictureFile("file://elsewhere/tmp/a.png", "/work"))
}

// TestWaitingPromptFiles: the files of each waiting prompt go to the phone
// beside the queue, and /v1/media serves them by their mark.
func TestWaitingPromptFiles(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t, phoneAddr)
	env.src.setChat("s1", "Chat")
	env.src.mu.Lock()
	env.src.queued = []message.QueuedPrompt{
		{Prompt: "no files"},
		{Prompt: "look", Attachments: []message.Attachment{{FileName: "shot.png", MimeType: "image/png", Content: tinyPNG}}},
	}
	env.src.mu.Unlock()
	env.srv.SetPresence(Presence{SessionID: "s1"})

	snap := env.stream().next(t)
	var queue []string
	var files [][]Attachment
	require.NoError(t, json.Unmarshal(snap["queue"], &queue))
	require.NoError(t, json.Unmarshal(snap["queue_files"], &files))
	require.Equal(t, []string{"no files", "look"}, queue)
	require.Len(t, files, 2)
	require.Empty(t, files[0])
	require.Len(t, files[1], 1)
	require.Equal(t, "shot.png", files[1][0].Name)
	require.True(t, files[1][0].Image)

	resp, err := http.Get(env.url + "/v1/media/" + files[1][0].ID)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	require.Equal(t, tinyPNG, body)

	resp, err = http.Get(env.url + "/v1/media/queue/nothing")
	require.NoError(t, err)
	resp.Body.Close()
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}
