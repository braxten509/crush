package styles

import (
	"charm.land/glamour/v2/ansi"
	"charm.land/lipgloss/v2"
)

// quickStyleMarkdown sets the reply markdown style.
func quickStyleMarkdown(s *Styles, o quickStyleOpts) {
	s.Markdown = ansi.StyleConfig{
		Document: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				// BlockPrefix: "\n",
				// BlockSuffix: "\n",
				// Replies are in the full text color, like Codex: a
				// shaded body made the reply read as secondary.
				Color: hex(o.fgBase),
			},
			// Margin: new(uint(defaultMargin)),
		},
		BlockQuote: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{},
			Indent:         new(uint(1)),
			IndentToken:    new("│ "),
		},
		List: ansi.StyleList{
			LevelIndent: defaultListIndent,
		},
		Heading: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				BlockSuffix: "\n",
				Color:       hex(o.info),
				Bold:        new(true),
			},
		},
		// No background chip on headings, the same as inline code: text
		// on a colored block is harder to read.
		H1: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix: "# ",
			},
		},
		H2: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix: "## ",
			},
		},
		H3: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix: "### ",
			},
		},
		H4: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix: "#### ",
			},
		},
		H5: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix: "##### ",
			},
		},
		H6: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix: "###### ",
				Color:  hex(o.successMostSubtle),
				Bold:   new(false),
			},
		},
		Strikethrough: ansi.StylePrimitive{
			CrossedOut: new(true),
		},
		Emph: ansi.StylePrimitive{
			Italic: new(true),
		},
		Strong: ansi.StylePrimitive{
			Bold: new(true),
		},
		HorizontalRule: ansi.StylePrimitive{
			Color:  hex(o.separator),
			Format: "\n--------\n",
		},
		Item: ansi.StylePrimitive{
			BlockPrefix: "• ",
		},
		Enumeration: ansi.StylePrimitive{
			BlockPrefix: ". ",
		},
		Task: ansi.StyleTask{
			StylePrimitive: ansi.StylePrimitive{},
			Ticked:         "[✓] ",
			Unticked:       "[ ] ",
		},
		Link: ansi.StylePrimitive{
			Color:     hex(o.info),
			Underline: new(true),
		},
		LinkText: ansi.StylePrimitive{
			Color: hex(o.successMostSubtle),
			Bold:  new(true),
		},
		Image: ansi.StylePrimitive{
			Color:     hex(o.accent),
			Underline: new(true),
		},
		ImageText: ansi.StylePrimitive{
			Color:  hex(o.fgMoreSubtle),
			Format: "Image: {{.text}} →",
		},
		// Inline code is colored text only: no background and no padding,
		// like Codex. A chip background made code harder to read, and the
		// padding would show as stray spaces without it. Copies still get
		// the backticks back from the message source (list.HighlightSource).
		Code: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Color: hex(o.destructive),
			},
		},
		CodeBlock: ansi.StyleCodeBlock{
			StyleBlock: ansi.StyleBlock{
				StylePrimitive: ansi.StylePrimitive{
					Color: hex(o.bgLessVisible),
				},
				Margin: new(uint(defaultMargin)),
			},
			Chroma: &ansi.Chroma{
				Text: ansi.StylePrimitive{
					Color: hex(o.fgSubtle),
				},
				Error: ansi.StylePrimitive{
					Color:           hex(o.onPrimary),
					BackgroundColor: hex(o.error),
				},
				Comment: ansi.StylePrimitive{
					Color: hex(o.fgMostSubtle),
				},
				CommentPreproc: ansi.StylePrimitive{
					Color: hex(o.accent),
				},
				Keyword: ansi.StylePrimitive{
					Color: hex(o.info),
				},
				KeywordReserved: ansi.StylePrimitive{
					Color: hex(o.keyword),
				},
				KeywordNamespace: ansi.StylePrimitive{
					Color: hex(o.keyword),
				},
				KeywordType: ansi.StylePrimitive{
					Color: hex(o.infoMoreSubtle),
				},
				Operator: ansi.StylePrimitive{
					Color: hex(o.fgSubtle),
				},
				Punctuation: ansi.StylePrimitive{
					Color: hex(o.warningSubtle),
				},
				Name: ansi.StylePrimitive{
					Color: hex(o.fgSubtle),
				},
				NameBuiltin: ansi.StylePrimitive{
					Color: hex(o.accent),
				},
				NameTag: ansi.StylePrimitive{
					Color: hex(o.secondary),
				},
				NameAttribute: ansi.StylePrimitive{
					Color: hex(o.infoMoreSubtle),
				},
				NameClass: ansi.StylePrimitive{
					Color:     hex(o.primary),
					Underline: new(true),
					Bold:      new(true),
				},
				NameDecorator: ansi.StylePrimitive{
					Color: hex(o.attention),
				},
				NameFunction: ansi.StylePrimitive{
					Color: hex(o.successMostSubtle),
				},
				LiteralNumber: ansi.StylePrimitive{
					Color: hex(o.success),
				},
				LiteralString: ansi.StylePrimitive{
					Color: hex(o.warningSubtle),
				},
				LiteralStringEscape: ansi.StylePrimitive{
					Color: hex(o.successMoreSubtle),
				},
				GenericDeleted: ansi.StylePrimitive{
					Color: hex(o.destructive),
				},
				GenericEmph: ansi.StylePrimitive{
					Italic: new(true),
				},
				GenericInserted: ansi.StylePrimitive{
					Color: hex(o.successMostSubtle),
				},
				GenericStrong: ansi.StylePrimitive{
					Bold: new(true),
				},
				GenericSubheading: ansi.StylePrimitive{
					Color: hex(o.fgMoreSubtle),
				},
				Background: ansi.StylePrimitive{
					BackgroundColor: hex(o.bgLessVisible),
				},
			},
		},
		Table: ansi.StyleTable{
			StyleBlock: ansi.StyleBlock{
				StylePrimitive: ansi.StylePrimitive{},
			},
		},
		DefinitionDescription: ansi.StylePrimitive{
			BlockPrefix: "\n ",
		},
	}
}

// quickStyleMarkdownVariants derives the plan and question markdown styles
// from the reply style.
func quickStyleMarkdownVariants(s *Styles, o quickStyleOpts, base lipgloss.Style) {
	// PlanMarkdown keeps the rich markdown colors for the plan card. H2–H5 in
	// s.Markdown only set Prefix and rely on glamour inheriting Color/Bold
	// from the base Heading style; copy them explicitly so section headings
	// stand out inside the card.
	planMD := s.Markdown
	headingColor := hex(o.info)
	headingBold := new(true)
	for _, h := range []*ansi.StyleBlock{&planMD.H1, &planMD.H2, &planMD.H3, &planMD.H4, &planMD.H5} {
		if h.Color == nil {
			h.Color = headingColor
		}
		if h.Bold == nil {
			h.Bold = headingBold
		}
	}
	// Replace raw markdown prefixes ("## ", "### ", …) with clean indentation so
	// the plan card doesn't show literal # / ## characters. H1 and H2 get no
	// prefix (they stand on their own with bold+color); H3–H5 use increasing
	// indentation for hierarchy.
	planMD.H1.Prefix = ""
	planMD.H2.Prefix = ""
	planMD.H3.Prefix = "  "
	planMD.H4.Prefix = "    "
	planMD.H5.Prefix = "      "
	planMD.Code.Color = hex(o.destructive)
	planMD.Code.Bold = new(true)
	planMD.CodeBlock.Color = hex(o.fgBase)
	s.PlanMarkdown = planMD

	// QuestionMarkdown is the reply markdown with dimmed body text, for a
	// question's description, so it reads as secondary to the question.
	questionMD := s.Markdown
	questionMD.Document.Color = hex(o.fgMoreSubtle)
	s.QuestionMarkdown = questionMD
}

// quickStyleQuietMarkdown sets the muted markdown style of thinking content.
func quickStyleQuietMarkdown(s *Styles, o quickStyleOpts, muted, subtle lipgloss.Style) {
	// QuietMarkdown style - muted colors on subtle background for thinking content.
	plainBg := hex(o.bgLeastVisible)
	plainFg := hex(o.fgMoreSubtle)
	s.QuietMarkdown = ansi.StyleConfig{
		Document: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Color:           plainFg,
				BackgroundColor: plainBg,
			},
		},
		BlockQuote: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Color:           plainFg,
				BackgroundColor: plainBg,
			},
			Indent:      new(uint(1)),
			IndentToken: new("│ "),
		},
		List: ansi.StyleList{
			LevelIndent: defaultListIndent,
		},
		Heading: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				BlockSuffix:     "\n",
				Bold:            new(true),
				Color:           plainFg,
				BackgroundColor: plainBg,
			},
		},
		H1: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix:          " ",
				Suffix:          " ",
				Bold:            new(true),
				Color:           plainFg,
				BackgroundColor: plainBg,
			},
		},
		H2: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix:          "## ",
				Color:           plainFg,
				BackgroundColor: plainBg,
			},
		},
		H3: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix:          "### ",
				Color:           plainFg,
				BackgroundColor: plainBg,
			},
		},
		H4: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix:          "#### ",
				Color:           plainFg,
				BackgroundColor: plainBg,
			},
		},
		H5: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix:          "##### ",
				Color:           plainFg,
				BackgroundColor: plainBg,
			},
		},
		H6: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix:          "###### ",
				Color:           plainFg,
				BackgroundColor: plainBg,
			},
		},
		Strikethrough: ansi.StylePrimitive{
			CrossedOut:      new(true),
			Color:           plainFg,
			BackgroundColor: plainBg,
		},
		Emph: ansi.StylePrimitive{
			Italic:          new(true),
			Color:           plainFg,
			BackgroundColor: plainBg,
		},
		Strong: ansi.StylePrimitive{
			Bold:            new(true),
			Color:           plainFg,
			BackgroundColor: plainBg,
		},
		HorizontalRule: ansi.StylePrimitive{
			Format:          "\n--------\n",
			Color:           plainFg,
			BackgroundColor: plainBg,
		},
		Item: ansi.StylePrimitive{
			BlockPrefix:     "• ",
			Color:           plainFg,
			BackgroundColor: plainBg,
		},
		Enumeration: ansi.StylePrimitive{
			BlockPrefix:     ". ",
			Color:           plainFg,
			BackgroundColor: plainBg,
		},
		Task: ansi.StyleTask{
			StylePrimitive: ansi.StylePrimitive{
				Color:           plainFg,
				BackgroundColor: plainBg,
			},
			Ticked:   "[✓] ",
			Unticked: "[ ] ",
		},
		Link: ansi.StylePrimitive{
			Underline:       new(true),
			Color:           plainFg,
			BackgroundColor: plainBg,
		},
		LinkText: ansi.StylePrimitive{
			Bold:            new(true),
			Color:           plainFg,
			BackgroundColor: plainBg,
		},
		Image: ansi.StylePrimitive{
			Underline:       new(true),
			Color:           plainFg,
			BackgroundColor: plainBg,
		},
		ImageText: ansi.StylePrimitive{
			Format:          "Image: {{.text}} →",
			Color:           plainFg,
			BackgroundColor: plainBg,
		},
		// Inline code is the one primitive that must NOT take plainFg: with
		// the same color as the surrounding text it would be invisible as
		// code. A slightly brighter foreground marks it without a chip
		// background, which made it harder to read.
		Code: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Color: hex(o.fgSubtle),
			},
		},
		CodeBlock: ansi.StyleCodeBlock{
			StyleBlock: ansi.StyleBlock{
				StylePrimitive: ansi.StylePrimitive{
					Color:           plainFg,
					BackgroundColor: plainBg,
				},
				Margin: new(uint(defaultMargin)),
			},
		},
		Table: ansi.StyleTable{
			StyleBlock: ansi.StyleBlock{
				StylePrimitive: ansi.StylePrimitive{
					Color:           plainFg,
					BackgroundColor: plainBg,
				},
			},
		},
		DefinitionDescription: ansi.StylePrimitive{
			BlockPrefix:     "\n ",
			Color:           plainFg,
			BackgroundColor: plainBg,
		},
	}
}
