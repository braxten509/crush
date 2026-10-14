package model

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/charmbracelet/crush/internal/session"
)

// TestTodoSectionHasNoBoxOrQueue checks the to-do section draws without
// boxes and leaves queued messages to the composer, however it is opened
// or focused.
func TestTodoSectionHasNoBoxOrQueue(t *testing.T) {
	incompleteTodos := []session.Todo{{Content: "a", Status: session.TodoStatusPending}}

	cases := []struct {
		name           string
		expanded       bool
		focusedSection pillSection
		todos          []session.Todo
	}{
		{"queue only", false, pillSectionTodos, nil},
		{"collapsed queue+todos", false, pillSectionTodos, incompleteTodos},
		{"expanded queue focused", true, pillSectionQueue, incompleteTodos},
		{"expanded todos focused", true, pillSectionTodos, incompleteTodos},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			u := newTestUI()
			u.session = &session.Session{ID: "s1", Todos: tc.todos}
			u.queue.count = 2
			u.queue.items = []string{"queued one", "queued two"}
			u.pillsExpanded = tc.expanded
			u.focusedPillSection = tc.focusedSection
			u.updateLayoutAndSize()
			u.renderPills()

			if strings.ContainsAny(u.pillsView, "╭╮╰╯") {
				t.Fatalf("the to-do section has no box:\n%s", u.pillsView)
			}
			if strings.Contains(u.pillsView, "queued") {
				t.Fatalf("queued messages belong above the composer:\n%s", u.pillsView)
			}
			if tc.todos == nil && u.pillsView != "" {
				t.Fatalf("no to-dos, no section:\n%s", u.pillsView)
			}
			if tc.expanded && tc.todos != nil && !strings.Contains(ansi.Strip(u.pillsView), "• a") {
				t.Fatalf("an open section lists the to-dos whichever part was focused:\n%s", u.pillsView)
			}
		})
	}
}

// TestEffectiveFocusedSectionFallsThrough verifies that a stale focused section
// (pointing at a section with no content) resolves to the section that still
// has content, so the expanded list stays populated.
func TestEffectiveFocusedSectionFallsThrough(t *testing.T) {
	cases := []struct {
		name     string
		stored   pillSection
		todos    []session.Todo
		queue    int
		expected pillSection
	}{
		{"todos focus but only queue", pillSectionTodos, nil, 2, pillSectionQueue},
		{"queue focus but only todos", pillSectionQueue, []session.Todo{{Content: "a", Status: session.TodoStatusPending}}, 0, pillSectionTodos},
		{"todos focus with todos", pillSectionTodos, []session.Todo{{Content: "a", Status: session.TodoStatusPending}}, 2, pillSectionTodos},
		{"queue focus with queue", pillSectionQueue, nil, 2, pillSectionQueue},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			u := newTestUI()
			u.session = &session.Session{ID: "s1", Todos: tc.todos}
			u.queue.count = tc.queue
			u.focusedPillSection = tc.stored
			if got := u.effectiveFocusedSection(); got != tc.expected {
				t.Fatalf("effectiveFocusedSection() = %d, want %d", got, tc.expected)
			}
		})
	}
}
