package sessionhost

import (
	"image"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
	"github.com/stretchr/testify/require"
)

// newTestHost returns a host of the given size with sessions that have no
// process behind them, showing the first.
func newTestHost(t *testing.T, width, height int, statuses ...Status) *Host {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	h := New(Options{})
	h.width, h.height = width, height
	for i, st := range statuses {
		s := newSession(i+1, 10, 5, nil, nil)
		s.status = st
		h.sessions = append(h.sessions, s)
	}
	h.show(0)
	return h
}

func screenText(h *Host) string {
	return ansi.Strip(h.View().Content)
}

func press(h *Host, key string) {
	var k tea.Key
	switch key {
	case "enter":
		k.Code = tea.KeyEnter
	case "esc":
		k.Code = tea.KeyEscape
	case "right":
		k.Code = tea.KeyRight
	default:
		mod, letter, ok := strings.Cut(key, "+")
		if !ok {
			letter, mod = key, ""
		}
		k.Code, k.Text = rune(letter[0]), ""
		if mod == "alt" {
			k.Mod = tea.ModAlt
		}
	}
	h.Update(tea.KeyPressMsg(k))
}

func TestPrefsMissingFileMeansOpen(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "crush", "session-list.json")
	require.False(t, loadPrefs(path).ListClosed)
	require.NoError(t, savePrefs(path, prefs{ListClosed: true}))
	require.True(t, loadPrefs(path).ListClosed)
}

func TestToggleListIsRemembered(t *testing.T) {
	h := newTestHost(t, 120, 30, Status{Title: "One"})
	require.True(t, h.listOpen())
	press(h, "alt+s")
	require.False(t, h.listOpen())

	again := New(Options{})
	again.width = 120
	require.False(t, again.listOpen(), "a new host starts the way the list was left")
	press(h, "alt+s")
	again = New(Options{})
	again.width = 120
	require.True(t, again.listOpen())
}

func TestNarrowTerminalShowsStripWithoutChangingSetting(t *testing.T) {
	h := newTestHost(t, minWidthForList-1, 30, Status{Title: "One"})
	require.False(t, h.listOpen())
	require.False(t, h.prefs.ListClosed)
	require.Equal(t, stripWidth, h.sideWidth())
}

func TestRowAt(t *testing.T) {
	h := newTestHost(t, 120, 30, Status{Title: "One"}, Status{Title: "Two"})
	require.True(t, h.rowAt(toggleColumn, 0).toggle)
	for x := 1; x < listWidth-2; x++ {
		require.True(t, h.rowAt(x, newSessionRow).newSession)
		require.False(t, h.rowAt(x, newSessionRow).toggle)
		require.Equal(t, -1, h.rowAt(x, newSessionRow).session)
	}
	require.False(t, h.rowAt(0, newSessionRow).newSession)
	require.False(t, h.rowAt(listWidth-2, newSessionRow).newSession)
	require.False(t, h.rowAt(newSessionColumn, 2).newSession)
	require.False(t, h.rowAt(2, 0).toggle)
	require.False(t, h.rowAt(2, 0).newSession)
	require.Equal(t, -1, h.rowAt(3, 1).session)

	r := h.rowAt(5, listFirstRow)
	require.Equal(t, 0, r.session)
	require.False(t, r.close)
	require.True(t, h.rowAt(listWidth-3, listFirstRow).close)
	require.Equal(t, 0, h.rowAt(5, listFirstRow+1).session)
	require.Equal(t, -1, h.rowAt(5, listFirstRow+2).session, "the gap between sessions")
	require.Equal(t, 1, h.rowAt(5, listFirstRow+listBlockHeight).session)

	press(h, "alt+s")
	require.True(t, h.rowAt(1, 0).toggle)
	require.True(t, h.rowAt(1, 1).newSession)
	require.Equal(t, 1, h.rowAt(1, stripFirstRow+stripBlockHeight+1).session)
	require.False(t, h.rowAt(1, stripFirstRow).close, "the strip closes with alt+w only")
}

func TestClickSwitchesSession(t *testing.T) {
	h := newTestHost(t, 120, 30, Status{Title: "One"}, Status{Title: "Two"})
	h.Update(tea.MouseClickMsg{X: 5, Y: listFirstRow + listBlockHeight, Button: tea.MouseLeft})
	require.Equal(t, 1, h.active)
	require.True(t, h.sessions[1].visible.Load())
	require.False(t, h.sessions[0].visible.Load())
}

func TestAltNumberSwitchesSession(t *testing.T) {
	h := newTestHost(t, 120, 30, Status{Title: "One"}, Status{Title: "Two"}, Status{Title: "Three"})
	press(h, "alt+3")
	require.Equal(t, 2, h.active)
	press(h, "alt+9")
	require.Equal(t, 2, h.active, "a number without a session does nothing")
}

func TestClosingAlwaysAsks(t *testing.T) {
	h := newTestHost(t, 120, 30, Status{Title: "Ready one", State: StateReady}, Status{Title: "Two"})

	press(h, "alt+w")
	require.NotNil(t, h.confirm, "even a ready session asks first")
	text := screenText(h)
	require.Contains(t, text, "Close “Ready one”?")
	require.Contains(t, text, "The chat stays saved")
	press(h, "esc")
	require.Nil(t, h.confirm)
	require.False(t, h.sessions[0].stopped)

	press(h, "alt+w")
	press(h, "right")
	press(h, "enter")
	require.False(t, h.sessions[0].stopped, "Keep was selected")

	h.Update(tea.MouseClickMsg{X: listWidth - 3, Y: listFirstRow, Button: tea.MouseLeft})
	require.NotNil(t, h.confirm, "the × asks too")
	press(h, "enter")
	require.True(t, h.sessions[0].stopped)
	require.Contains(t, screenText(h), "× Ready one", "a closing session shows ×")
}

func TestCloseBoxSaysWhatTheSessionIsDoing(t *testing.T) {
	h := newTestHost(t, 120, 30, Status{Title: "Busy", State: StateWorking})
	press(h, "alt+w")
	require.Contains(t, screenText(h), "It is still working.")
	press(h, "esc")
	h.sessions[0].status.State = StateWaiting
	press(h, "alt+w")
	require.Contains(t, screenText(h), "waiting for your answer")
}

func TestCloseBoxFitsItsText(t *testing.T) {
	h := newTestHost(t, 120, 30, Status{Title: "One"}, Status{Title: "Two", State: StateWorking})
	for i, st := range []Status{h.sessions[0].snapshot(), h.sessions[1].snapshot()} {
		press(h, "alt+"+string(rune('1'+i)))
		press(h, "alt+w")
		box, closeButton, _ := h.confirmBox(h.sessionArea(), confirmDetail(st))
		lines := strings.Split(screenText(h), "\n")
		require.Contains(t, lines[closeButton.Min.Y], "Close")
		require.Contains(t, lines[closeButton.Min.Y], "Keep")
		require.Contains(t, lines[box.Max.Y-1], "╰", "the border closes right below the buttons")
		press(h, "esc")
	}
}

func TestCloseBoxButtonsClick(t *testing.T) {
	h := newTestHost(t, 120, 30, Status{Title: "One"})
	press(h, "alt+w")
	_, closeButton, keepButton := h.confirmBox(h.sessionArea(), confirmDetail(h.sessions[0].snapshot()))
	h.Update(tea.MouseClickMsg{X: keepButton.Min.X, Y: keepButton.Min.Y, Button: tea.MouseLeft})
	require.Nil(t, h.confirm)
	require.False(t, h.sessions[0].stopped)
	press(h, "alt+w")
	h.Update(tea.MouseClickMsg{X: closeButton.Min.X, Y: closeButton.Min.Y, Button: tea.MouseLeft})
	require.True(t, h.sessions[0].stopped)
}

func TestFinishedWhileHiddenIsUnreadUntilShown(t *testing.T) {
	h := newTestHost(t, 120, 30, Status{Title: "Shown"}, Status{Title: "Other"})
	other := h.sessions[1]
	other.status.State = StateWorking
	h.statusChanged(other.id)
	other.status.State = StateReady
	h.statusChanged(other.id)
	require.True(t, other.unread)
	require.Contains(t, screenText(h), "✓ Other")

	press(h, "alt+2")
	require.False(t, other.unread)
}

func TestExitedSessionLeavesAndLastOneQuits(t *testing.T) {
	h := newTestHost(t, 120, 30, Status{Title: "One"}, Status{Title: "Two"})
	press(h, "alt+2")
	_, cmd := h.Update(exitedMsg{id: 2})
	require.Nil(t, cmd)
	require.Len(t, h.sessions, 1)
	require.Equal(t, 0, h.active)
	require.True(t, h.sessions[0].visible.Load())

	_, cmd = h.Update(exitedMsg{id: 1})
	require.NotNil(t, cmd)
	_, quit := cmd().(tea.QuitMsg)
	require.True(t, quit)
}

func TestStripShowsOneMarkPerSession(t *testing.T) {
	h := newTestHost(t, 120, 30,
		Status{Title: "One", State: StateWorking},
		Status{Title: "Two", State: StateWaiting},
		Status{Title: "Three", State: StateReady},
	)
	press(h, "alt+s")
	lines := strings.Split(screenText(h), "\n")
	var marks []string
	for _, line := range lines[stripFirstRow:] {
		if cell := strings.Trim(ansi.Cut(line, 0, stripWidth-1), " ▌"); cell != "" && !strings.ContainsAny(cell, "0123456789") {
			marks = append(marks, cell)
		}
	}
	require.Equal(t, []string{"●", "!", "○"}, marks)
}

func TestListShowsSessionDetails(t *testing.T) {
	home, _ := os.UserHomeDir()
	h := newTestHost(t, 120, 30,
		Status{Title: "Remote setup", Dir: filepath.Join(home, "dev", "crush"), Model: "GPT-6.1 Sol · High", State: StateWorking},
		Status{Title: "Inbox cleanup", State: StateWaiting},
		Status{Title: "Notes", Dir: filepath.Join(home, "notes"), Model: "Opus · High", State: StateReady},
	)
	text := screenText(h)
	for _, want := range []string{"Sessions [3]", "+ New session", "‹", "● Remote setup", "  crush", "! Inbox cleanup",
		"○ Notes", "  notes", "alt+n new · alt+s hide"} {
		require.Contains(t, text, want)
	}
	// Only the folder's name shows below the title: no model, no words.
	for _, gone := range []string{"GPT-6.1 Sol", "Opus", "~/dev/crush", "Working", "Needs you", "Viewing", "Ready"} {
		require.NotContains(t, text, gone)
	}
	for _, line := range strings.Split(text, "\n") {
		require.LessOrEqual(t, ansi.StringWidth(line), 120)
	}
}

func TestStatusSequenceThroughEmulator(t *testing.T) {
	t.Parallel()
	want := Status{Title: "Fix \x1b]0;bad\x07 title; with ;", Dir: "/tmp/x", Model: "Opus", Theme: "graphite", State: StateWaiting}
	emu := vt.NewSafeEmulator(20, 5)
	var got Status
	emu.RegisterOscHandler(statusOSC, func(data []byte) bool {
		var ok bool
		got, ok = parseStatus(data)
		return ok
	})
	_, err := emu.Write([]byte("before" + want.Sequence() + "after"))
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestChildEnviron(t *testing.T) {
	t.Parallel()
	env := ChildEnviron([]string{
		"HOME=/home/me", "TERM=xterm-kitty", "TERM_PROGRAM=ghostty", "KONSOLE_VERSION=1",
		"KITTY_WINDOW_ID=3", "CRUSH_CRASH_REPORT=/tmp/r", "PATH=/bin",
	}, "CRUSH_CRASH_REPORT")
	require.Equal(t, []string{"HOME=/home/me", "PATH=/bin", "TERM=xterm-256color", "COLORTERM=truecolor", ChildEnv + "=1"}, env)
	require.False(t, slices.ContainsFunc(env, func(kv string) bool { return strings.HasPrefix(kv, "KONSOLE_") }))
}

func TestCopyReachesTheRealClipboard(t *testing.T) {
	t.Parallel()
	s := newSession(1, 20, 5, nil, nil)
	_, err := s.emu.Write([]byte(ansi.SetSystemClipboard("copied text")))
	require.NoError(t, err)
	require.True(t, s.copied.Load())
	require.Equal(t, "copied text", s.clip)

	h := New(Options{})
	_, cmd := h.Update(clipboardMsg{text: s.clip})
	require.NotNil(t, cmd, "the host writes it to the real clipboard")
}

// readInput returns what the session's Crush would read next.
func readInput(t *testing.T, s *session) string {
	t.Helper()
	got := make(chan string, 1)
	go func() {
		buf := make([]byte, 64)
		n, _ := s.emu.Read(buf)
		got <- string(buf[:n])
	}()
	select {
	case text := <-got:
		return text
	case <-time.After(2 * time.Second):
		t.Fatal("the session got no input")
		return ""
	}
}

func TestHiddenSessionLosesFocus(t *testing.T) {
	h := newTestHost(t, 120, 30, Status{Title: "One"}, Status{Title: "Two"})
	for _, s := range h.sessions {
		_, err := s.emu.Write([]byte(ansi.SetModeFocusEvent))
		require.NoError(t, err)
		require.True(t, s.focusEvents.Load())
	}
	press(h, "alt+2")
	require.Equal(t, ansi.Blur, readInput(t, h.sessions[0]), "the session now hidden")
	require.Equal(t, ansi.Focus, readInput(t, h.sessions[1]), "the session now shown")

	h.Update(tea.BlurMsg{})
	require.Equal(t, ansi.Blur, readInput(t, h.sessions[1]))
	h.Update(tea.FocusMsg{})
	require.Equal(t, ansi.Focus, readInput(t, h.sessions[1]))
}

func TestShiftEnterBecomesCtrlJ(t *testing.T) {
	t.Parallel()
	text, key := sessionInput(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModShift})
	require.Empty(t, text)
	require.Equal(t, uv.KeyPressEvent{Code: 'j', Mod: uv.ModCtrl}, key)
	_, plain := sessionInput(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.Equal(t, rune(tea.KeyEnter), plain.Code)

	s := newSession(1, 20, 5, nil, nil)
	// Sending waits until Crush reads, as the host's input loop does.
	go s.emu.SendKey(key)
	require.Equal(t, "\n", readInput(t, s), "Ctrl+J reaches Crush as a line feed")
}

func TestTypedTextReachesSession(t *testing.T) {
	for _, tc := range []struct {
		name string
		key  tea.KeyPressMsg
		want string
	}{
		{"letter", tea.KeyPressMsg{Code: 'a', Text: "a"}, "a"},
		{"shift letter", tea.KeyPressMsg{Code: 'a', ShiftedCode: 'A', Text: "A", Mod: tea.ModShift}, "A"},
		{"shift digit", tea.KeyPressMsg{Code: '1', ShiftedCode: '!', Text: "!", Mod: tea.ModShift}, "!"},
		{"caps lock", tea.KeyPressMsg{Code: 'a', Text: "A", Mod: tea.ModCapsLock}, "A"},
		{"num lock", tea.KeyPressMsg{Code: '5', Text: "5", Mod: tea.ModNumLock}, "5"},
		{"other layout", tea.KeyPressMsg{Code: 'é', Text: "é"}, "é"},
		{"space", tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}, " "},
		{"alt shift letter", tea.KeyPressMsg{Code: 'a', ShiftedCode: 'A', Mod: tea.ModAlt | tea.ModShift}, "\x1bA"},
		{"alt letter", tea.KeyPressMsg{Code: 'x', Mod: tea.ModAlt}, "\x1bx"},
		{"ctrl letter", tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}, "\x03"},
		{"shift tab", tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}, "\x1b[Z"},
		{"arrow", tea.KeyPressMsg{Code: tea.KeyUp}, "\x1b[A"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newTestHost(t, 120, 30, Status{Title: "One"})
			got := make(chan string, 1)
			go func() { got <- readInput(t, h.sessions[0]) }()
			h.Update(tc.key)
			require.Equal(t, tc.want, <-got)
		})
	}
}

// The emulator would pass every mouse move on; the host passes only the
// ones Crush asked for: none, moves with a button held, or every move.
func TestSessionGetsOnlyTheMouseMovesItAskedFor(t *testing.T) {
	h := newTestHost(t, 120, 30, Status{Title: "One"})
	s := h.sessions[0]
	input := make(chan string, 16)
	go func() {
		buf := make([]byte, 64)
		for {
			n, err := s.emu.Read(buf)
			if err != nil {
				return
			}
			input <- string(buf[:n])
		}
	}()
	t.Cleanup(func() { _ = s.emu.Close() })
	none := func(why string) {
		t.Helper()
		select {
		case got := <-input:
			t.Fatalf("%s, but the session got %q", why, got)
		case <-time.After(200 * time.Millisecond):
		}
	}
	some := func(why string) {
		t.Helper()
		select {
		case <-input:
		case <-time.After(2 * time.Second):
			t.Fatal(why)
		}
	}
	move := tea.MouseMotionMsg{X: 50, Y: 0}
	drag := tea.MouseMotionMsg{X: 50, Y: 0, Button: tea.MouseLeft}

	_, err := s.emu.Write([]byte(ansi.SetModeMouseButtonEvent + ansi.SetModeMouseExtSgr))
	require.NoError(t, err)
	h.Update(move)
	none("a move with no button held isn't asked for")
	h.Update(drag)
	some("a drag is asked for")

	_, err = s.emu.Write([]byte(ansi.SetModeMouseAnyEvent))
	require.NoError(t, err)
	h.Update(move)
	some("every move is asked for")
}

// inputs returns everything the session's Crush reads, as it comes.
func inputs(t *testing.T, s *session) <-chan string {
	t.Helper()
	got := make(chan string, 16)
	go func() {
		buf := make([]byte, 256)
		for {
			n, err := s.emu.Read(buf)
			if err != nil {
				return
			}
			got <- string(buf[:n])
		}
	}()
	t.Cleanup(func() { _ = s.emu.Close() })
	return got
}

func nextInput(t *testing.T, in <-chan string) string {
	t.Helper()
	select {
	case text := <-in:
		return text
	case <-time.After(2 * time.Second):
		t.Fatal("the session got no input")
		return ""
	}
}

func noInput(t *testing.T, in <-chan string) {
	t.Helper()
	select {
	case text := <-in:
		t.Fatalf("the session got %q", text)
	case <-time.After(300 * time.Millisecond):
	}
}

const pictureQuestion = "\x1b_Gi=31,s=1,v=1,a=q,t=d,f=24;AAAA\x1b\\"

func TestPictureQuestionAnsweredOnlyWhenTheTerminalShowsPictures(t *testing.T) {
	s := newSession(1, 40, 10, nil, nil)
	in := inputs(t, s)
	_, err := s.emu.Write([]byte(pictureQuestion))
	require.NoError(t, err)
	noInput(t, in)

	s.kitty.Store(true)
	_, err = s.emu.Write([]byte(pictureQuestion))
	require.NoError(t, err)
	require.Equal(t, "\x1b_Gi=31;OK\x1b\\", nextInput(t, in))
}

// placePicture is what Crush writes to put picture id at a cell: move the
// cursor there, then send the picture in two parts.
func placePicture(id string, col, row int) string {
	return ansi.CursorPosition(col+1, row+1) +
		"\x1b_Ga=T,f=100,i=" + id + ",c=4,r=2,C=1,q=2,m=1;AAAA\x1b\\\x1b_Gm=0;BBBB\x1b\\"
}

func TestSessionNotesWherePicturesGo(t *testing.T) {
	s := newSession(1, 40, 10, nil, nil)
	t.Cleanup(func() { _ = s.emu.Close() })
	s.kitty.Store(true)
	_, err := s.emu.Write([]byte(placePicture("7", 4, 2) + "\x1b_Ga=T,U=1,i=9,q=2;CCCC\x1b\\" + "\x1b_Ga=d,d=I,i=7,q=2\x1b\\"))
	require.NoError(t, err)
	require.True(t, s.pictured.Load())
	cmds := s.pictures
	require.Len(t, cmds, 4)
	require.True(t, cmds[0].place && cmds[0].first)
	require.Equal(t, "7", cmds[0].id)
	require.Equal(t, image.Pt(4, 2), cmds[0].at)
	require.True(t, cmds[1].place && !cmds[1].first, "the second part goes with the first")
	require.Equal(t, image.Pt(4, 2), cmds[1].at)
	require.False(t, cmds[2].place, "a placeholder picture has no place of its own")
	require.True(t, cmds[3].remove)
	require.Equal(t, "7", cmds[3].id)
}

// pictureOutput runs msg through the host and returns what it wrote to the
// real terminal.
func pictureOutput(h *Host, msg tea.Msg) string {
	h.update(msg)
	h.syncPictures()
	return h.takeRaw()
}

func TestPicturesFollowTheShownSession(t *testing.T) {
	h := newTestHost(t, 120, 30, Status{Title: "One"}, Status{Title: "Two"})
	pictureOutput(h, nil)
	one, two := h.sessions[0], h.sessions[1]
	place := func(id string) []pictureCmd {
		return []pictureCmd{
			{seq: "<" + id + "-1>", place: true, first: true, id: id, at: image.Pt(4, 2)},
			{seq: "<" + id + "-2>", place: true, id: id, at: image.Pt(4, 2)},
		}
	}
	at := ansi.SaveCursor + ansi.CursorPosition(4+listWidth+1, 3)
	shown := at + "<7-1>" + ansi.RestoreCursor + at + "<7-2>" + ansi.RestoreCursor
	gone := ansi.KittyGraphics(nil, "a=d", "d=i", "i=7", "q=2")

	require.Equal(t, shown, pictureOutput(h, picturesMsg{id: one.id, cmds: place("7")}),
		"placed past the list")
	require.Empty(t, pictureOutput(h, picturesMsg{id: two.id, cmds: place("8")}),
		"a hidden session's picture waits")
	require.Equal(t, "<9>", pictureOutput(h, picturesMsg{id: two.id, cmds: []pictureCmd{{seq: "<9>"}}}),
		"a placeholder picture goes out right away")

	out := pictureOutput(h, tea.KeyPressMsg(tea.Key{Code: '2', Mod: tea.ModAlt}))
	require.True(t, strings.HasPrefix(out, gone), "switching takes the old picture down")
	require.Contains(t, out, "<8-1>", "and puts the new session's up")

	pictureOutput(h, tea.KeyPressMsg(tea.Key{Code: '1', Mod: tea.ModAlt}))
	require.Equal(t, gone, pictureOutput(h, tea.KeyPressMsg(tea.Key{Code: 'n', Mod: tea.ModAlt})),
		"the new-session box covers it")
	require.Equal(t, shown, pictureOutput(h, tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape})),
		"and it comes back after")

	out = pictureOutput(h, tea.KeyPressMsg(tea.Key{Code: 's', Mod: tea.ModAlt}))
	require.Contains(t, out, gone)
	require.Contains(t, out, ansi.CursorPosition(4+stripWidth+1, 3)+"<7-1>", "moved with the narrower list")

	require.Equal(t, ansi.KittyGraphics(nil, "a=d", "d=I", "i=7", "q=2"),
		pictureOutput(h, picturesMsg{id: one.id, cmds: []pictureCmd{{seq: ansi.KittyGraphics(nil, "a=d", "d=I", "i=7", "q=2"), remove: true, id: "7"}}}))
	require.Empty(t, one.placed, "a removed picture isn't put back")
}

func TestHostSharesPictureSupportAndCellSize(t *testing.T) {
	h := newTestHost(t, 120, 30, Status{Title: "One"}, Status{Title: "Two"})
	h.Update(uv.KittyGraphicsEvent{})
	h.Update(uv.PixelSizeEvent{Width: 1200, Height: 600})
	for _, s := range h.sessions {
		require.True(t, s.kitty.Load())
		require.EqualValues(t, 10, s.cellWidth.Load())
		require.EqualValues(t, 20, s.cellHeight.Load())
	}
}

func TestSessionLearnsItsSizeInPixels(t *testing.T) {
	s := newSession(1, 40, 10, nil, nil)
	t.Cleanup(func() { _ = s.emu.Close() })
	s.cellWidth.Store(9)
	s.cellHeight.Store(18)
	_, err := s.emu.Write([]byte("\x1b[14t"))
	require.NoError(t, err)
	require.Equal(t, "\x1b[4;180;360t", readInput(t, s))
}

func TestNewSessionPlusSharesTheHeadingColumn(t *testing.T) {
	h := newTestHost(t, 120, 30, Status{Title: "One"})
	scr := uv.NewScreenBuffer(120, 30)
	h.drawList(scr, h.palette())
	heading, plus := -1, -1
	for x := 0; x < listWidth; x++ {
		if c := scr.CellAt(x, 0); c != nil && c.Content == "S" {
			heading = x
		}
		if c := scr.CellAt(x, newSessionRow); c != nil && c.Content == "+" {
			plus = x
		}
	}
	require.NotEqual(t, -1, heading)
	require.Equal(t, heading, plus)
	require.True(t, h.rowAt(plus, newSessionRow).newSession, "the aligned icon remains clickable")
}
