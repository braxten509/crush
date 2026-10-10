package vt

import (
	"bytes"
	"io"
	"sync"
)

// inputBuffer queues terminal input without waiting for the process to read it.
// Storage grows only with unread input: backpressure cannot block the emulator
// or discard bytes. No forwarding goroutine is needed. Once closed, readers
// drain accepted input before receiving EOF and new writes fail.
type inputBuffer struct {
	mu     sync.Mutex
	ready  *sync.Cond
	buffer bytes.Buffer
	closed bool
}

func newInputBuffer() *inputBuffer {
	b := new(inputBuffer)
	b.ready = sync.NewCond(&b.mu)
	return b
}

func (b *inputBuffer) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for b.buffer.Len() == 0 && !b.closed {
		b.ready.Wait()
	}
	if b.buffer.Len() == 0 {
		return 0, io.EOF
	}
	n, err := b.buffer.Read(p)
	if b.buffer.Len() == 0 {
		// Release large paste allocations after the reader catches up.
		b.buffer = bytes.Buffer{}
	}
	return n, err
}

func (b *inputBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return 0, io.ErrClosedPipe
	}
	n, err := b.buffer.Write(p) // Copies the caller's input.
	if n > 0 {
		b.ready.Broadcast()
	}
	return n, err
}

func (b *inputBuffer) WriteString(s string) (int, error) {
	return b.writeStrings(s)
}

// writeStrings keeps framing and payload together even when InputPipe callers
// write concurrently with a bracketed paste.
func (b *inputBuffer) writeStrings(parts ...string) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return 0, io.ErrClosedPipe
	}
	n := 0
	for _, part := range parts {
		written, _ := b.buffer.WriteString(part)
		n += written
	}
	if n > 0 {
		b.ready.Broadcast()
	}
	return n, nil
}

func (b *inputBuffer) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	b.ready.Broadcast()
	return nil
}
