package diffview

import (
	"fmt"
	"image/color"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/charmbracelet/crush/internal/ui/xchroma"
	"github.com/zeebo/xxh3"
)

func (dv *DiffView) hightlightCode(source string, bgColor color.Color) string {
	if dv.chromaStyle == nil {
		return source
	}

	// Create cache key from content and background color
	cacheKey := dv.createSyntaxCacheKey(source, bgColor)

	// Check if we already have this highlighted
	if cached, exists := dv.syntaxCache[cacheKey]; exists {
		return cached
	}

	l := dv.getChromaLexer()
	f := dv.getChromaFormatter(bgColor)

	it, err := l.Tokenise(nil, source)
	if err != nil {
		return source
	}

	var b strings.Builder
	if err := f.Format(&b, dv.chromaStyle, it); err != nil {
		return source
	}

	result := b.String()

	// Cache the result for future use
	dv.syntaxCache[cacheKey] = result

	return result
}

// createSyntaxCacheKey creates a cache key from source content and background color.
// We use a simple hash to keep memory usage reasonable.
func (dv *DiffView) createSyntaxCacheKey(source string, bgColor color.Color) string {
	// Convert color to string representation
	r, g, b, a := bgColor.RGBA()
	colorStr := fmt.Sprintf("%d,%d,%d,%d", r, g, b, a)

	// Create a hash of the content + color to use as cache key
	h := xxh3.New()
	h.Write([]byte(source))
	h.Write([]byte(colorStr))
	return fmt.Sprintf("%x", h.Sum(nil))
}

func (dv *DiffView) getChromaLexer() chroma.Lexer {
	if dv.cachedLexer != nil {
		return dv.cachedLexer
	}

	// MatchLexer is memoized and already coalesced; the per-DiffView cache
	// still avoids repeat lookups within a single render.
	if l := xchroma.MatchLexer(dv.before.path); l != nil {
		dv.cachedLexer = l
		return dv.cachedLexer
	}
	l := lexers.Analyse(dv.before.content)
	if l == nil {
		l = lexers.Fallback
	}
	dv.cachedLexer = chroma.Coalesce(l)
	return dv.cachedLexer
}

func (dv *DiffView) getChromaFormatter(bgColor color.Color) chroma.Formatter {
	return xchroma.Formatter(bgColor, processChromaValue)
}

func processChromaValue(value string) string {
	value = strings.TrimRight(value, "\n")
	value = escapeControl(value)
	return value
}
