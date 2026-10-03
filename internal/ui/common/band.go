package common

import (
	"image/color"
	"regexp"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

var sgrPattern = regexp.MustCompile(`\x1b\[([0-9;:]*)m`)

// OnBand paints a full-width background band behind an already styled line.
// Styled spans end with resets that would clear the band, so the band is put
// back after every reset of the background.
func OnBand(line string, width int, bg color.Color) string {
	if bg == nil {
		return line
	}
	on := ansi.NewStyle().BackgroundColor(bg).String()
	line = sgrPattern.ReplaceAllStringFunc(line, func(seq string) string {
		if clearsBackground(sgrPattern.FindStringSubmatch(seq)[1]) {
			return seq + on
		}
		return seq
	})
	if pad := width - ansi.StringWidth(line); pad > 0 {
		line += strings.Repeat(" ", pad)
	}
	return on + line + "\x1b[m"
}

// clearsBackground reports whether SGR parameters reset the background:
// a full reset or 49. Color arguments of 38/48/58 are skipped so their
// numbers aren't mistaken for resets.
func clearsBackground(params string) bool {
	if params == "" {
		return true
	}
	parts := strings.Split(params, ";")
	for i := 0; i < len(parts); i++ {
		p := parts[i]
		if strings.Contains(p, ":") {
			continue // colon form keeps a color's arguments in one part
		}
		switch p {
		case "", "0", "49":
			return true
		case "38", "48", "58":
			if i+1 < len(parts) {
				switch parts[i+1] {
				case "5":
					i += 2
				case "2":
					i += 4
				}
			}
		}
	}
	return false
}
