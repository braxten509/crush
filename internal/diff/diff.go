package diff

import (
	"strings"

	"github.com/aymanbagabas/go-udiff"
	"github.com/charmbracelet/crush/internal/linediff"
)

// GenerateDiff creates a unified diff from two file contents
func GenerateDiff(beforeContent, afterContent, fileName string) (string, int, int) {
	fileName = strings.TrimPrefix(fileName, "/")

	var (
		unified, _ = udiff.ToUnified("a/"+fileName, "b/"+fileName, beforeContent, linediff.Edits(beforeContent, afterContent), udiff.DefaultContextLines)
		additions  = 0
		removals   = 0
	)

	lines := strings.SplitSeq(unified, "\n")
	for line := range lines {
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			additions++
		} else if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---") {
			removals++
		}
	}

	return unified, additions, removals
}
