package model

import (
	"image"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/ui/completions"
	uv "github.com/charmbracelet/ultraviolet"
)

// mentionCompletions is the popup that completes an @ mention typed in the
// editor with a file or an MCP resource, or a $ mention with a skill.
type mentionCompletions struct {
	popup *completions.Completions
	open  bool
	// prefix is the character that opened the popup: "@" or "$".
	prefix string
	// skills are the names a $ popup offers.
	skills []completions.SkillCompletionValue
	// startIndex is where in the editor text the @ was typed, query what
	// follows it.
	startIndex int
	query      string
	// position is the screen position where the user typed the prefix.
	position image.Point
}

// triggers reports whether typing key into the editor holding value opens
// the popup: an @ or $ at the beginning of the prompt or after whitespace.
func (c *mentionCompletions) triggers(key, value string) bool {
	if (key != "@" && key != skillMentionPrefix) || c.open {
		return false
	}
	idx := len(value)
	return idx == 0 || isWhitespace(value[idx-1])
}

// begin opens the popup for an @ typed at startIndex, shown at position.
func (c *mentionCompletions) begin(startIndex int, position image.Point, depth, limit int) tea.Cmd {
	c.start("@", startIndex, position)
	return c.popup.Open(depth, limit)
}

// beginSkills opens the popup listing skills for a $ typed at startIndex,
// shown at position.
func (c *mentionCompletions) beginSkills(startIndex int, position image.Point, skills []completions.SkillCompletionValue) {
	c.start(skillMentionPrefix, startIndex, position)
	c.skills = skills
	c.popup.SetSkills(skills)
}

// offersSkill reports whether a skill name holds query. The popup's fuzzy
// match alone would keep it open on almost any word ($HOME finds
// "playwright-component-testing"), and Enter would then replace the word
// instead of sending.
func (c *mentionCompletions) offersSkill(query string) bool {
	query = strings.ToLower(query)
	for _, skill := range c.skills {
		if strings.Contains(strings.ToLower(skill.Name), query) {
			return true
		}
	}
	return false
}

// start marks the popup open for prefix typed at startIndex.
func (c *mentionCompletions) start(prefix string, startIndex int, position image.Point) {
	c.open = true
	c.prefix = prefix
	c.query = ""
	c.startIndex = startIndex
	c.position = position
}

// follow keeps an open popup in step with the editor after key changed its
// text to value, with word the word at the cursor: the popup filters on the
// word after the prefix, and closes once the cursor moves before it, on a
// space, or when the word no longer is a mention. A skill popup also closes
// once no skill name holds the word, so Enter sends the prompt. The keystroke that
// opened the popup is skipped since its items may still be loading.
func (c *mentionCompletions) follow(key, value, word string) {
	if !c.open || key == c.prefix {
		return
	}
	switch {
	case len(value) <= c.startIndex:
		// The cursor moved before the start.
		c.close()
	case key == "space":
		c.close()
	case strings.HasPrefix(word, c.prefix):
		c.query = word[len(c.prefix):]
		c.popup.Filter(c.query)
		if c.prefix == skillMentionPrefix && (!c.popup.HasItems() || !c.offersSkill(c.query)) {
			c.close()
		}
	default:
		c.close()
	}
}

// setItems fills an open @ popup with the items loaded for it.
func (c *mentionCompletions) setItems(msg completions.CompletionItemsLoadedMsg) {
	if c.open && c.prefix == "@" {
		c.popup.SetItems(msg.Files, msg.Resources)
	}
}

// close closes the popup and resets its state.
func (c *mentionCompletions) close() {
	c.open = false
	c.query = ""
	c.startIndex = 0
	c.skills = nil
	c.popup.Close()
}

// draw draws an open popup with items just above where the prefix was typed,
// kept within a screen screenW cells wide.
func (c *mentionCompletions) draw(scr uv.Screen, screenW int) {
	if !c.open || !c.popup.HasItems() {
		return
	}
	w, h := c.popup.Size()
	x := c.position.X
	y := c.position.Y - h

	if x+w > screenW {
		x = screenW - w
	}
	x = max(0, x)
	y = max(0, y+1) // Offset for attachments row

	completionsView := uv.NewStyledString(c.popup.Render())
	completionsView.Draw(scr, image.Rectangle{
		Min: image.Pt(x, y),
		Max: image.Pt(x+w, y+h),
	})
}
