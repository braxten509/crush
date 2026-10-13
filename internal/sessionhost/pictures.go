package sessionhost

import (
	"image"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// placedPicture is a picture a session put on screen: where, and the
// commands that put it there, to put it back.
type placedPicture struct {
	at   image.Point
	seqs []string
}

// picturesOn returns the session whose pictures may be on screen: the shown
// one, unless a box of the host's covers it.
func (h *Host) picturesOn() *session {
	if h.confirm != nil || h.picker != nil {
		return nil
	}
	s := h.current()
	if s != nil && s.kind == terminalSession && s.scrollOffset > 0 && !s.emu.IsAltScreen() {
		return nil
	}
	return s
}

// screenPictures is what the host has put on the real terminal with Kitty
// graphics, and what it still has to write there.
type screenPictures struct {
	// shown is the session whose pictures are on screen, placed for a list
	// side wide.
	shown *session
	side  int
	// raw is what to write to the real terminal after this update.
	raw strings.Builder
}

// takePictures follows a session's Kitty graphics commands and passes on
// what belongs on screen now.
func (h *Host) takePictures(s *session, cmds []pictureCmd) {
	h.pictures.take(s, cmds, h.sideWidth())
}

// take follows a session's Kitty graphics commands for a list side wide.
func (p *screenPictures) take(s *session, cmds []pictureCmd, side int) {
	on := s == p.shown && p.side == side
	for _, c := range cmds {
		switch {
		case c.place:
			placed := s.placed[c.id]
			if placed == nil || c.first {
				placed = &placedPicture{at: c.at}
				s.placed[c.id] = placed
			}
			placed.seqs = append(placed.seqs, c.seq)
			if on {
				p.raw.WriteString(placeAt(c.seq, c.at, side))
			}
		case c.remove:
			if c.id == "" {
				clear(s.placed)
			} else {
				delete(s.placed, c.id)
			}
			if on {
				p.raw.WriteString(c.seq)
			}
		default:
			// Pictures sent without a place (placeholder ones) can go
			// out whether or not the session is shown.
			p.raw.WriteString(c.seq)
		}
	}
}

// placeAt wraps a command that puts a picture at the cursor so it lands at
// the session's cell at, right of a list side wide, leaving the real cursor
// where it was.
func placeAt(seq string, at image.Point, side int) string {
	return ansi.SaveCursor + ansi.CursorPosition(at.X+side+1, at.Y+1) + seq + ansi.RestoreCursor
}

// syncPictures puts on screen the pictures of the session that should show
// them, and takes down those of a session that no longer should, or that
// moved because the list changed width.
func (h *Host) syncPictures() {
	h.pictures.sync(h.picturesOn(), h.sideWidth())
}

// sync shows want's pictures right of a list side wide, taking down the
// ones shown before when they differ.
func (p *screenPictures) sync(want *session, side int) {
	if want == p.shown && side == p.side {
		return
	}
	if old := p.shown; old != nil {
		for id := range old.placed {
			p.raw.WriteString(ansi.KittyGraphics(nil, "a=d", "d=i", "i="+id, "q=2"))
		}
	}
	p.shown, p.side = want, side
	if want == nil {
		return
	}
	for _, placed := range want.placed {
		for _, seq := range placed.seqs {
			p.raw.WriteString(placeAt(seq, placed.at, side))
		}
	}
}

// takeRaw returns what is waiting to be written to the real terminal.
func (p *screenPictures) takeRaw() string {
	out := p.raw.String()
	p.raw.Reset()
	return out
}
