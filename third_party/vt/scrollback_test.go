package vt

import (
	"sync"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
)

func TestScrollback(t *testing.T) {
	t.Run("basic push and len", func(t *testing.T) {
		sb := NewScrollback(100)
		if sb.Len() != 0 {
			t.Errorf("expected len 0, got %d", sb.Len())
		}
		if sb.MaxLines() != 100 {
			t.Errorf("expected max 100, got %d", sb.MaxLines())
		}
	})

	t.Run("scrollback in emulator", func(t *testing.T) {
		// Create a small terminal
		e := NewEmulator(10, 5)

		// Fill the screen with numbered lines and force scrolling
		for i := 0; i < 10; i++ {
			e.WriteString("\r\n") // Scroll up
		}

		// Check scrollback has captured some lines
		sbLen := e.ScrollbackLen()
		t.Logf("scrollback length after 10 newlines: %d", sbLen)

		if sbLen == 0 {
			t.Error("expected scrollback to have captured lines, got 0")
		}
	})

	t.Run("scrollback with content", func(t *testing.T) {
		e := NewEmulator(20, 5)

		// Write content that will scroll
		for i := 0; i < 10; i++ {
			e.WriteString("line\r\n")
		}

		// Verify scrollback captured the scrolled content
		sb := e.Scrollback()
		if sb == nil {
			t.Fatal("scrollback is nil")
		}

		// Should have captured lines (at least 5, since screen is 5 tall and we wrote 10 lines)
		if sb.Len() < 5 {
			t.Errorf("expected at least 5 lines in scrollback, got %d", sb.Len())
		}
	})

	t.Run("scrollback max lines", func(t *testing.T) {
		sb := NewScrollback(5)

		// Push more lines than max
		for i := 0; i < 10; i++ {
			sb.Push(nil)
		}

		if sb.Len() != 5 {
			t.Errorf("expected len 5 after overflow, got %d", sb.Len())
		}
	})

	t.Run("clear scrollback", func(t *testing.T) {
		e := NewEmulator(20, 5)

		// Write content that will scroll
		for i := 0; i < 10; i++ {
			e.WriteString("line\r\n")
		}

		// Verify we have scrollback
		if e.ScrollbackLen() == 0 {
			t.Error("expected scrollback before clear")
		}

		// Clear it
		e.ClearScrollback()

		if e.ScrollbackLen() != 0 {
			t.Errorf("expected empty scrollback after clear, got %d", e.ScrollbackLen())
		}
	})

	t.Run("alt screen does not have scrollback", func(t *testing.T) {
		e := NewEmulator(20, 5)

		// Write some content to main screen
		for i := 0; i < 10; i++ {
			e.WriteString("line\r\n")
		}

		mainScrollbackLen := e.ScrollbackLen()
		if mainScrollbackLen == 0 {
			t.Error("expected scrollback on main screen")
		}

		// Enter alt screen
		e.WriteString("\x1b[?1049h") // DECSET alt screen

		// Scrollback should still be from main screen
		if e.ScrollbackLen() != mainScrollbackLen {
			t.Errorf("expected scrollback len %d in alt screen, got %d",
				mainScrollbackLen, e.ScrollbackLen())
		}

		// Write to alt screen - should not affect main scrollback
		for i := 0; i < 10; i++ {
			e.WriteString("alt\r\n")
		}

		// Main screen scrollback should be unchanged
		if e.ScrollbackLen() != mainScrollbackLen {
			t.Errorf("expected scrollback len %d after alt screen writes, got %d",
				mainScrollbackLen, e.ScrollbackLen())
		}
	})

	t.Run("ED 2 saves to scrollback", func(t *testing.T) {
		e := NewEmulator(20, 5)

		// Write some content (not enough to scroll)
		e.WriteString("line 1\r\n")
		e.WriteString("line 2\r\n")
		e.WriteString("line 3\r\n")

		// Should have no scrollback yet (didn't scroll)
		initialLen := e.ScrollbackLen()

		// Clear screen with ED 2 (ESC[2J)
		e.WriteString("\x1b[2J")

		// Should have saved lines to scrollback
		newLen := e.ScrollbackLen()
		if newLen <= initialLen {
			t.Errorf("expected scrollback to grow after ED 2, was %d now %d", initialLen, newLen)
		}
		t.Logf("scrollback after ED 2: %d lines", newLen)
	})

	t.Run("ED 3 clears scrollback", func(t *testing.T) {
		e := NewEmulator(20, 5)

		// Write content that will scroll
		for i := 0; i < 10; i++ {
			e.WriteString("line\r\n")
		}

		// Verify we have scrollback
		if e.ScrollbackLen() == 0 {
			t.Error("expected scrollback before ED 3")
		}

		// ED 3 (ESC[3J) should clear scrollback
		e.WriteString("\x1b[3J")

		if e.ScrollbackLen() != 0 {
			t.Errorf("expected empty scrollback after ED 3, got %d", e.ScrollbackLen())
		}
	})
}

func TestScrollbackStateAtCapacityAndAfterClear(t *testing.T) {
	e := NewSafeEmulator(4, 1)
	t.Cleanup(func() { _ = e.Close() })
	e.SetScrollbackSize(2)
	_, _ = e.WriteString("A\r\nB\r\nC")
	length, total := e.ScrollbackState()
	if length != 2 || total != 2 {
		t.Fatalf("initial state = (%d, %d), want (2, 2)", length, total)
	}
	snapshot := e.Scrollback()
	destination := uv.NewScreenBuffer(4, 1)
	offset := 1
	e.DrawViewport(destination, destination.Bounds(), offset)
	if got := destination.CellAt(0, 0).Content; got != "B" {
		t.Fatalf("initial viewed row = %q, want B", got)
	}
	_, _ = e.WriteString("\r\nD")
	newLength, newTotal := e.ScrollbackState()
	if newLength != 2 || newTotal != 3 {
		t.Fatalf("state after eviction = (%d, %d), want (2, 3)", newLength, newTotal)
	}
	offset = min(offset+int(newTotal-total), newLength)
	e.DrawViewport(destination, destination.Bounds(), offset)
	if got := destination.CellAt(0, 0).Content; got != "B" {
		t.Errorf("anchored row after eviction = %q, want B", got)
	}
	if snapshot.Len() != 2 || snapshot.total != 2 || snapshot.CellAt(0, 0).Content != "A" {
		t.Error("later output changed the saved history snapshot")
	}
	snapshot.Clear()
	snapshot.Push(uv.Line{{Content: "private", Width: 1}})
	if snapshot.total != 3 {
		t.Error("snapshot did not preserve its appended-row counter")
	}
	if gotLength, gotTotal := e.ScrollbackState(); gotLength != 2 || gotTotal != 3 {
		t.Error("snapshot mutation changed emulator state")
	}
	e.ClearScrollback()
	if gotLength, gotTotal := e.ScrollbackState(); gotLength != 0 || gotTotal != 3 {
		t.Errorf("state after clear = (%d, %d), want (0, 3)", gotLength, gotTotal)
	}
	_, _ = e.WriteString("\r\nE\r\nF")
	if gotLength, gotTotal := e.ScrollbackState(); gotLength != 2 || gotTotal != 5 {
		t.Errorf("state after further output = (%d, %d), want (2, 5)", gotLength, gotTotal)
	}
	e.SetScrollbackSize(1)
	if gotLength, gotTotal := e.ScrollbackState(); gotLength != 1 || gotTotal != 5 {
		t.Errorf("state after reducing capacity = (%d, %d), want (1, 5)", gotLength, gotTotal)
	}
	_, _ = e.WriteString("\x1b[?1049hALT\r\n")
	if gotLength, gotTotal := e.ScrollbackState(); gotLength != 1 || gotTotal != 5 {
		t.Error("alternate-screen output changed main history state")
	}
}

func TestScrollbackCountsOnlyAppendedRows(t *testing.T) {
	var missing *Scrollback
	missing.Push(nil) // No history exists, so no row can be appended.
	sb := &Scrollback{maxLines: 0}
	sb.Push(nil)
	if sb.total != 0 {
		t.Error("disabled history counted a row")
	}
	sb = NewScrollback(2)
	sb.Push(nil) // Blank rows still represent actual scrolling.
	sb.Push(nil)
	sb.Push(nil)
	if sb.Len() != 2 || sb.total != 3 {
		t.Errorf("blank history state = (%d, %d), want (2, 3)", sb.Len(), sb.total)
	}
	sb.Clear()
	if sb.total != 3 {
		t.Error("clear reset total appended rows")
	}
}

func TestSafeScrollbackStateConcurrentSnapshots(t *testing.T) {
	e := NewSafeEmulator(4, 1)
	t.Cleanup(func() { _ = e.Close() })
	e.SetScrollbackSize(3)
	var workers sync.WaitGroup
	workers.Add(2)
	go func() {
		defer workers.Done()
		for i := 0; i < 200; i++ {
			_, _ = e.WriteString("X\r\n")
		}
	}()
	go func() {
		defer workers.Done()
		for i := 0; i < 200; i++ {
			length, total := e.ScrollbackState()
			if length != int(min(total, 3)) {
				t.Errorf("inconsistent state snapshot (%d, %d)", length, total)
			}
			snapshot := e.Scrollback()
			if snapshot.Len() != int(min(snapshot.total, 3)) {
				t.Errorf("inconsistent history snapshot (%d, %d)", snapshot.Len(), snapshot.total)
			}
			if cell := snapshot.CellAt(0, 0); cell != nil {
				cell.Content = "private"
			}
		}
	}()
	workers.Wait()
	if length, total := e.ScrollbackState(); length != 3 || total != 200 {
		t.Errorf("final state = (%d, %d), want (3, 200)", length, total)
	}
}
