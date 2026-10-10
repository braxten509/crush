package vt

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

func awaitInputWork(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("terminal work blocked without an input reader")
	}
}

func TestInputQueuesLargeWritesAndOwnsBytes(t *testing.T) {
	e := NewEmulator(4, 2)
	t.Cleanup(func() { _ = e.Close() })
	payload := bytes.Repeat([]byte{0, 1, 2, 3, 255}, 400000)
	want := bytes.Clone(payload)
	done := make(chan struct{})
	var written int
	var writeError error
	go func() {
		written, writeError = e.InputPipe().Write(payload)
		close(done)
	}()
	awaitInputWork(t, done)
	if written != len(payload) || writeError != nil {
		t.Fatalf("write = (%d, %v), want (%d, nil)", written, writeError, len(payload))
	}
	clear(payload)
	e.SendText("tail")
	_ = e.Close()
	got, err := io.ReadAll(e)
	if err != nil || !bytes.Equal(got, append(want, []byte("tail")...)) {
		t.Fatalf("queued input changed or dropped: read %d bytes, error %v", len(got), err)
	}
	if e.input.buffer.Cap() != 0 {
		t.Error("drained queue retained its allocation")
	}
	if n, err := e.InputPipe().Write([]byte("late")); n != 0 || !errors.Is(err, io.ErrClosedPipe) {
		t.Errorf("write after close = (%d, %v)", n, err)
	}
	if n, err := e.Write([]byte("late output")); n != 0 || !errors.Is(err, io.ErrClosedPipe) {
		t.Errorf("output after close = (%d, %v)", n, err)
	}
	if n, err := e.Read(make([]byte, 1)); n != 0 || !errors.Is(err, io.EOF) {
		t.Errorf("read after drain = (%d, %v)", n, err)
	}
}

func TestInputOrderingAcrossPasteKeysAndReplies(t *testing.T) {
	e := NewSafeEmulator(4, 2)
	t.Cleanup(func() { _ = e.Close() })
	done := make(chan struct{})
	go func() {
		_, _ = e.InputPipe().Write([]byte("raw"))
		e.SendText("text")
		_, _ = e.WriteString("\x1b[?2004h")
		e.Paste("pasted\ntext")
		e.SendKey(uv.KeyPressEvent{Code: uv.KeyUp})
		// A cursor report exercises output parsing that queues a reply while
		// the emulator lock is held. No shell reader has been started.
		_, _ = e.WriteString("\x1b[6n\x1b]10;?\x07")
		e.SendText("last")
		close(done)
	}()
	awaitInputWork(t, done)
	_ = e.Close()
	got, err := io.ReadAll(e)
	foreground := ansi.XRGBColor{Color: e.ForegroundColor()}
	want := "rawtext" + ansi.BracketedPasteStart + "pasted\ntext" + ansi.BracketedPasteEnd +
		"\x1b[A" + ansi.CursorPositionReport(1, 1) + ansi.SetForegroundColor(foreground.String()) + "last"
	if err != nil || string(got) != want {
		t.Errorf("input = %q, error %v; want %q", got, err, want)
	}
}

func TestInputCloseWakesReaders(t *testing.T) {
	e := NewSafeEmulator(4, 2)
	t.Cleanup(func() { _ = e.Close() })
	const readers = 8
	results := make(chan error, readers)
	started := make(chan struct{}, readers)
	for i := 0; i < readers; i++ {
		go func() {
			started <- struct{}{}
			_, err := e.Read(make([]byte, 1))
			results <- err
		}()
	}
	for i := 0; i < readers; i++ {
		<-started
	}
	_ = e.Close()
	_ = e.Close()
	for i := 0; i < readers; i++ {
		select {
		case err := <-results:
			if !errors.Is(err, io.EOF) {
				t.Errorf("reader returned %v, want EOF", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("close left a reader blocked")
		}
	}
}

func TestInputReaderWakesAndReadsAcrossWrites(t *testing.T) {
	queue := newInputBuffer()
	t.Cleanup(func() { _ = queue.Close() })
	if n, err := queue.Read(nil); n != 0 || err != nil {
		t.Fatalf("empty read = (%d, %v), want (0, nil)", n, err)
	}
	done := make(chan struct{})
	got := make([]byte, 6)
	var readError error
	go func() {
		_, readError = io.ReadFull(queue, got)
		close(done)
	}()
	_, _ = queue.Write([]byte("abc"))
	_, _ = queue.WriteString("def")
	awaitInputWork(t, done)
	if readError != nil || string(got) != "abcdef" {
		t.Errorf("read = %q, error %v; want abcdef", got, readError)
	}
}

func TestInputConcurrentOutputAndDrawingWithoutReader(t *testing.T) {
	e := NewSafeEmulator(12, 3)
	t.Cleanup(func() { _ = e.Close() })
	_, _ = e.WriteString("\x1b[?2004h")
	payload := strings.Repeat("p", 1024*1024)
	var workers sync.WaitGroup
	workers.Add(3)
	go func() {
		defer workers.Done()
		e.Paste(payload)
		for i := 0; i < 100; i++ {
			e.SendKey(uv.KeyPressEvent{Code: 'k'})
			e.SendText("t")
		}
	}()
	go func() {
		defer workers.Done()
		for i := 0; i < 100; i++ {
			_, _ = e.WriteString("X\r\n\x1b[6n")
		}
	}()
	go func() {
		defer workers.Done()
		destination := uv.NewScreenBuffer(12, 3)
		for i := 0; i < 100; i++ {
			e.DrawViewport(destination, destination.Bounds(), i)
			_, _ = e.InputPipe().Write([]byte("r"))
		}
	}()
	done := make(chan struct{})
	go func() {
		workers.Wait()
		close(done)
	}()
	awaitInputWork(t, done)
	_ = e.Close()
	got, err := io.ReadAll(e)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(got, []byte(ansi.BracketedPasteStart+payload+ansi.BracketedPasteEnd)) {
		t.Error("concurrent input split or dropped the paste")
	}
	for _, marker := range []byte{'k', 't', 'r'} {
		if count := bytes.Count(got, []byte{marker}); count != 100 {
			t.Errorf("marker %q count = %d, want 100", marker, count)
		}
	}
	if count := bytes.Count(got, []byte("R")); count != 100 {
		t.Errorf("cursor reply count = %d, want 100", count)
	}
}

func TestInputConcurrentClosePreservesAcceptedWrites(t *testing.T) {
	queue := newInputBuffer()
	var writers sync.WaitGroup
	const count = 8
	writers.Add(count)
	accepted := make(chan int, count)
	for i := 0; i < count; i++ {
		go func() {
			defer writers.Done()
			written := 0
			for i := 0; i < 100; i++ {
				n, err := queue.WriteString("record")
				written += n
				if err != nil {
					if !errors.Is(err, io.ErrClosedPipe) {
						t.Errorf("write error %v", err)
					}
					break
				}
			}
			accepted <- written
		}()
	}
	_, _ = queue.WriteString("record")
	_ = queue.Close()
	writers.Wait()
	want := len("record")
	for i := 0; i < count; i++ {
		want += <-accepted
	}
	got, err := io.ReadAll(queue)
	if err != nil || len(got) != want || strings.ReplaceAll(string(got), "record", "") != "" {
		t.Errorf("accepted %d bytes, drained %d bytes, error %v", want, len(got), err)
	}
}
