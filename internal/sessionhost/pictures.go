package sessionhost

import (
	"image"

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
	return h.current()
}

// takePictures follows a session's Kitty graphics commands and passes on
// what belongs on screen now.
func (h *Host) takePictures(s *session, cmds []pictureCmd) {
	on := s == h.shownPictures && h.picturesSide == h.sideWidth()
	for _, c := range cmds {
		switch {
		case c.place:
			p := s.placed[c.id]
			if p == nil || c.first {
				p = &placedPicture{at: c.at}
				s.placed[c.id] = p
			}
			p.seqs = append(p.seqs, c.seq)
			if on {
				h.raw.WriteString(h.placeAt(c.seq, c.at))
			}
		case c.remove:
			if c.id == "" {
				clear(s.placed)
			} else {
				delete(s.placed, c.id)
			}
			if on {
				h.raw.WriteString(c.seq)
			}
		default:
			// Pictures sent without a place (placeholder ones) can go
			// out whether or not the session is shown.
			h.raw.WriteString(c.seq)
		}
	}
}

// placeAt wraps a command that puts a picture at the cursor so it lands at
// the session's cell at, leaving the real cursor where it was.
func (h *Host) placeAt(seq string, at image.Point) string {
	return ansi.SaveCursor + ansi.CursorPosition(at.X+h.sideWidth()+1, at.Y+1) + seq + ansi.RestoreCursor
}

// syncPictures puts on screen the pictures of the session that should show
// them, and takes down those of a session that no longer should, or that
// moved because the list changed width.
func (h *Host) syncPictures() {
	want, side := h.picturesOn(), h.sideWidth()
	if want == h.shownPictures && side == h.picturesSide {
		return
	}
	if old := h.shownPictures; old != nil {
		for id := range old.placed {
			h.raw.WriteString(ansi.KittyGraphics(nil, "a=d", "d=i", "i="+id, "q=2"))
		}
	}
	h.shownPictures, h.picturesSide = want, side
	if want == nil {
		return
	}
	for _, p := range want.placed {
		for _, seq := range p.seqs {
			h.raw.WriteString(h.placeAt(seq, p.at))
		}
	}
}

// takeRaw returns what is waiting to be written to the real terminal.
func (h *Host) takeRaw() string {
	out := h.raw.String()
	h.raw.Reset()
	return out
}
