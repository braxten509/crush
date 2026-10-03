package chat

import (
	"html"
	"strings"

	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// CopySource supplies individual visible blocks, with code verbatim from the
// markdown tree. Hidden markup, plan markers and reasoning are never sources.
func (a *AssistantMessageItem) CopySource() []string {
	if a.message.IsCompacting || a.message.IsSummaryMessage {
		return nil
	}
	source := []byte(common.StripPlanMarkers(a.message.Content().Text))
	document := goldmark.New(goldmark.WithExtensions(extension.GFM)).Parser().Parse(text.NewReader(source))
	var blocks []string
	_ = ast.Walk(document, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch node.(type) {
		case *ast.FencedCodeBlock, *ast.CodeBlock:
			blocks = append(blocks, string(node.Lines().Value(source)))
			return ast.WalkSkipChildren, nil
		case *ast.HTMLBlock:
			return ast.WalkSkipChildren, nil
		}
		if child := node.FirstChild(); node.Type() == ast.TypeBlock && child != nil && child.Type() == ast.TypeInline {
			blocks = append(blocks, copyInlineSource(node, source))
			return ast.WalkSkipChildren, nil
		}
		return ast.WalkContinue, nil
	})
	return blocks
}

func copyInlineSource(block ast.Node, source []byte) string {
	var out strings.Builder
	_ = ast.Walk(block, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		switch node := node.(type) {
		case *ast.RawHTML:
			return ast.WalkSkipChildren, nil
		case *ast.Text:
			if entering {
				value := node.Segment.Value(source)
				if node.IsRaw() {
					out.Write(value)
				} else {
					out.WriteString(html.UnescapeString(string(util.UnescapePunctuations(value))))
				}
				if node.HardLineBreak() {
					out.WriteByte('\n')
				} else if node.SoftLineBreak() {
					out.WriteByte(' ')
				}
			}
		case *ast.String:
			if entering {
				out.Write(node.Value)
			}
		case *ast.CodeSpan:
			out.WriteByte('`')
		case *ast.AutoLink:
			if entering {
				out.Write(node.Label(source))
			}
		}
		return ast.WalkContinue, nil
	})
	return out.String()
}
