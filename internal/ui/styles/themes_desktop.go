package styles

import "charm.land/lipgloss/v2"

// The desktop app's two built-in themes (desktop design 5-6, 3.7). Both
// apps list them, so a theme the desktop shows can be picked and edited in
// the terminal too.

// scholarsEditionOpts returns the Scholar's edition: the desktop's default
// light theme, ink on warm paper with one vermilion accent.
func scholarsEditionOpts() quickStyleOpts {
	return quickStyleOpts{
		primary:   lipgloss.Color("#e52b0e"),
		secondary: lipgloss.Color("#14323f"),
		accent:    lipgloss.Color("#d9290d"),
		keyword:   lipgloss.Color("#7c4a1e"),

		fgBase:       lipgloss.Color("#0f0f0f"),
		fgSubtle:     lipgloss.Color("#383634"),
		fgMoreSubtle: lipgloss.Color("#353533"),
		fgMostSubtle: lipgloss.Color("#6f6e6f"),

		bgBase:         lipgloss.Color("#f9f8ef"),
		bgMostVisible:  lipgloss.Color("#edeee8"),
		bgLessVisible:  lipgloss.Color("#f3f1e6"),
		bgLeastVisible: lipgloss.Color("#fbfaf2"),

		onPrimary: lipgloss.Color("#f9f8ef"),
		separator: lipgloss.Color("#b0afa9"),

		destructive:       lipgloss.Color("#a1261b"),
		error:             lipgloss.Color("#a1261b"),
		warning:           lipgloss.Color("#e0a523"),
		warningSubtle:     lipgloss.Color("#8a5a00"),
		attention:         lipgloss.Color("#a64b00"),
		busy:              lipgloss.Color("#8a5a00"),
		info:              lipgloss.Color("#14323f"),
		infoMoreSubtle:    lipgloss.Color("#2f5d73"),
		infoMostSubtle:    lipgloss.Color("#4b6b78"),
		success:           lipgloss.Color("#3d6b3a"),
		successMoreSubtle: lipgloss.Color("#4f7a3f"),
		successMostSubtle: lipgloss.Color("#557a4d"),

		diffInsertFg:       lipgloss.Color("#2f5a1e"),
		diffInsertCodeBg:   lipgloss.Color("#eaf0dc"),
		diffInsertGutterBg: lipgloss.Color("#dce6c8"),
		diffDeleteFg:       lipgloss.Color("#a1261b"),
		diffDeleteCodeBg:   lipgloss.Color("#f6e6df"),
		diffDeleteGutterBg: lipgloss.Color("#efd5ca"),

		yolo:           lipgloss.Color("#8a5a00"),
		plan:           lipgloss.Color("#2f5d73"),
		planMoreSubtle: lipgloss.Color("#6f8a96"),

		button:         lipgloss.Color("#e34524"),
		buttonSubtle:   lipgloss.Color("#fbfaf2"),
		buttonInactive: lipgloss.Color("#edeee8"),
		buttonHovered:  lipgloss.Color("#f3f1e6"),

		ansiBlack:   lipgloss.Color("#0f0f0f"),
		ansiRed:     lipgloss.Color("#a1261b"),
		ansiGreen:   lipgloss.Color("#3f6b22"),
		ansiYellow:  lipgloss.Color("#8a5a00"),
		ansiBlue:    lipgloss.Color("#1f4f8a"),
		ansiMagenta: lipgloss.Color("#7a3b7d"),
		ansiCyan:    lipgloss.Color("#1d6670"),
		ansiWhite:   lipgloss.Color("#6e6d6e"),

		ansiBrightBlack:   lipgloss.Color("#5f5e5a"),
		ansiBrightRed:     lipgloss.Color("#c2341f"),
		ansiBrightGreen:   lipgloss.Color("#4a7827"),
		ansiBrightYellow:  lipgloss.Color("#946400"),
		ansiBrightBlue:    lipgloss.Color("#2d63b0"),
		ansiBrightMagenta: lipgloss.Color("#924a95"),
		ansiBrightCyan:    lipgloss.Color("#23727c"),
		ansiBrightWhite:   lipgloss.Color("#4a4947"),
	}
}

// scholarsEditionOverrides gives the untouched edition the desktop's inline
// code and syntax colors, so code reads the same in both apps.
func scholarsEditionOverrides(s Styles) Styles {
	s.Markdown.Code.Color = hex(lipgloss.Color("#6a3d12"))
	if chroma := s.Markdown.CodeBlock.Chroma; chroma != nil {
		chroma.Keyword.Color = hex(lipgloss.Color("#0f0f0f"))
		chroma.LiteralString.Color = hex(lipgloss.Color("#3f5f22"))
		chroma.LiteralNumber.Color = hex(lipgloss.Color("#7a4a08"))
		chroma.Comment.Color = hex(lipgloss.Color("#5f5e5a"))
		chroma.NameFunction.Color = hex(lipgloss.Color("#14323f"))
		chroma.KeywordType.Color = hex(lipgloss.Color("#3e4f7a"))
		chroma.NameAttribute.Color = hex(lipgloss.Color("#6a3d12"))
		chroma.CommentPreproc.Color = hex(lipgloss.Color("#5f5e5a"))
	}
	return s
}

// charcoalAmberOpts returns Charcoal amber: the desktop's dark theme, light
// gray on charcoal with amber accents. Its diff colors are derived.
func charcoalAmberOpts() quickStyleOpts {
	return quickStyleOpts{
		primary:   lipgloss.Color("#f8be43"),
		secondary: lipgloss.Color("#c9cbd0"),
		accent:    lipgloss.Color("#f0b455"),
		keyword:   lipgloss.Color("#e8a77a"),

		fgBase:       lipgloss.Color("#f0f0f0"),
		fgSubtle:     lipgloss.Color("#b6b8bc"),
		fgMoreSubtle: lipgloss.Color("#99999f"),
		fgMostSubtle: lipgloss.Color("#85858a"),

		bgBase:         lipgloss.Color("#181818"),
		bgMostVisible:  lipgloss.Color("#3f4042"),
		bgLessVisible:  lipgloss.Color("#282828"),
		bgLeastVisible: lipgloss.Color("#1d1e1f"),

		onPrimary: lipgloss.Color("#181818"),
		separator: lipgloss.Color("#3f4042"),

		destructive:       lipgloss.Color("#f46659"),
		error:             lipgloss.Color("#f46659"),
		warning:           lipgloss.Color("#f6bb41"),
		warningSubtle:     lipgloss.Color("#e0b45c"),
		attention:         lipgloss.Color("#f0b455"),
		busy:              lipgloss.Color("#f8be43"),
		info:              lipgloss.Color("#8fb8de"),
		infoMoreSubtle:    lipgloss.Color("#7aa2c8"),
		infoMostSubtle:    lipgloss.Color("#6b8aa8"),
		success:           lipgloss.Color("#6cc17a"),
		successMoreSubtle: lipgloss.Color("#8fd19b"),
		successMostSubtle: lipgloss.Color("#5aa86a"),

		yolo:           lipgloss.Color("#f8be43"),
		plan:           lipgloss.Color("#8cc7c7"),
		planMoreSubtle: lipgloss.Color("#4f7272"),

		button:         lipgloss.Color("#f0b455"),
		buttonSubtle:   lipgloss.Color("#282828"),
		buttonInactive: lipgloss.Color("#3f4042"),
		buttonHovered:  lipgloss.Color("#5c5e64"),

		ansiBlack:   lipgloss.Color("#181818"),
		ansiRed:     lipgloss.Color("#f46659"),
		ansiGreen:   lipgloss.Color("#6cc17a"),
		ansiYellow:  lipgloss.Color("#f6bb41"),
		ansiBlue:    lipgloss.Color("#7aa2c8"),
		ansiMagenta: lipgloss.Color("#c792ea"),
		ansiCyan:    lipgloss.Color("#7fcfcf"),
		ansiWhite:   lipgloss.Color("#b6b8bc"),

		ansiBrightBlack:   lipgloss.Color("#5c5e64"),
		ansiBrightRed:     lipgloss.Color("#ff8a7a"),
		ansiBrightGreen:   lipgloss.Color("#8fd19b"),
		ansiBrightYellow:  lipgloss.Color("#f8d27a"),
		ansiBrightBlue:    lipgloss.Color("#9cc3e6"),
		ansiBrightMagenta: lipgloss.Color("#ddb3f2"),
		ansiBrightCyan:    lipgloss.Color("#a3e3e3"),
		ansiBrightWhite:   lipgloss.Color("#f4f4f4"),
	}
}
