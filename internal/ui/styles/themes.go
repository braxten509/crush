package styles

import (
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/exp/charmtone"
)

// ThemeKeyForProvider returns a stable identifier for the theme
// associated with the given provider ID. Providers that share a theme
// yield the same key, so callers can cheaply detect when switching
// providers would not actually change the active theme and skip the
// expensive style rebuild. This is the single source of truth for the
// provider-to-theme mapping; [ThemeForProvider] builds on it.
func ThemeKeyForProvider(providerID string) string {
	switch providerID {
	case "hyper":
		return "hyper"
	default:
		return "default"
	}
}

// ThemeForProvider returns the Styles associated with the given provider
// ID. Unknown or empty provider IDs yield the default Charmtone Pantera
// theme.
func ThemeForProvider(providerID string) Styles {
	switch ThemeKeyForProvider(providerID) {
	case "hyper":
		return HypercrushObsidiana()
	default:
		return CharmtonePantera()
	}
}

// CharmtonePantera returns the Charmtone dark theme. It's the default style
// for the UI.
func CharmtonePantera() Styles {
	return charmtoneOverrides(quickStyle(charmtoneOpts()))
}

// charmtoneOpts returns the quickStyleOpts for the Charmtone dark theme,
// using colors from the upstream charmbracelet/x/exp/charmtone package.
func charmtoneOpts() quickStyleOpts {
	return quickStyleOpts{
		primary:   charmtone.Charple,
		secondary: charmtone.Dolly,
		accent:    charmtone.Bok,
		keyword:   charmtone.Blush,

		fgBase:       charmtone.Sash,
		fgMoreSubtle: charmtone.Squid,
		fgSubtle:     charmtone.Smoke,
		fgMostSubtle: charmtone.Oyster,

		onPrimary: charmtone.Butter,

		bgBase:         charmtone.Pepper,
		bgLeastVisible: charmtone.BBQ,
		bgLessVisible:  charmtone.Char,
		bgMostVisible:  charmtone.Iron,

		separator: charmtone.Char,

		destructive:       charmtone.Coral,
		error:             charmtone.Sriracha,
		warningSubtle:     charmtone.Zest,
		warning:           charmtone.Mustard,
		attention:         charmtone.Tang,
		busy:              charmtone.Citron,
		info:              charmtone.Malibu,
		infoMoreSubtle:    charmtone.Sardine,
		infoMostSubtle:    charmtone.Damson,
		success:           charmtone.Julep,
		successMoreSubtle: charmtone.Bok,
		successMostSubtle: charmtone.Guac,
		yolo:              charmtone.Zest,
		plan:              charmtone.Charple,
		planMoreSubtle:    charmtone.Hazy,

		// Muted diff foreground hues; the backgrounds derive from these
		// over bgBase.
		diffInsertFg: lipgloss.Color("#629657"),
		diffDeleteFg: lipgloss.Color("#a45c59"),

		button:         charmtone.Dolly,
		buttonSubtle:   charmtone.Char,
		buttonInactive: charmtone.Iron,
		buttonHovered:  charmtone.Oyster,
		// ANSI 16-color palette for remapping raw terminal output
		// (e.g. bang-mode shell commands) onto legible Charmtone colors.
		ansiBlack:   charmtone.BBQ,
		ansiRed:     charmtone.Coral,
		ansiGreen:   charmtone.Guac,
		ansiYellow:  charmtone.Mustard,
		ansiBlue:    charmtone.Charple,
		ansiMagenta: charmtone.Dolly,
		ansiCyan:    charmtone.Malibu,
		ansiWhite:   charmtone.Smoke,

		ansiBrightBlack:   charmtone.Iron,
		ansiBrightRed:     charmtone.Tuna,
		ansiBrightGreen:   charmtone.Julep,
		ansiBrightYellow:  charmtone.Zest,
		ansiBrightBlue:    charmtone.Guppy,
		ansiBrightMagenta: charmtone.Blush,
		ansiBrightCyan:    charmtone.Sardine,
		ansiBrightWhite:   charmtone.Salt,
	}
}

// charmtoneOverrides applies Charmtone-specific tweaks that don't fit the
// token model of [quickStyleOpts].
func charmtoneOverrides(s Styles) Styles {
	// Bang ! prompt overrides - use Salt/Hazy/Larple colors.
	s.Editor.PromptBangIconFocused = s.Editor.PromptBangIconFocused.
		Foreground(charmtone.Salt).
		Background(charmtone.Hazy)
	s.Editor.PromptBangDotsFocused = s.Editor.PromptBangDotsFocused.
		Foreground(charmtone.Hazy)
	s.Editor.PromptBangDotsBlurred = s.Editor.PromptBangDotsBlurred.
		Foreground(charmtone.Larple)

	// Shell bar/prompt overrides - use Charple/Iron/Hazy colors.
	s.Messages.ShellBarFocused = s.Messages.ShellBarFocused.
		BorderForeground(charmtone.Charple)
	s.Messages.ShellBarBlurred = s.Messages.ShellBarBlurred.
		BorderForeground(charmtone.Iron)
	s.Messages.ShellPrompt = s.Messages.ShellPrompt.
		Foreground(charmtone.Hazy)
	s.Messages.ShellPromptBlurred = s.Messages.ShellPromptBlurred.
		Foreground(charmtone.Hazy)

	// Restore the original Charmtone syntax-highlight and markdown colors
	// where the generic quickStyle token choices diverge from the palette
	// this theme has always used.
	chroma := s.Markdown.CodeBlock.Chroma
	if chroma != nil {
		chroma.CommentPreproc.Color = hex(charmtone.Bengal)
		chroma.KeywordReserved.Color = hex(charmtone.Pony)
		chroma.KeywordNamespace.Color = hex(charmtone.Pony)
		chroma.KeywordType.Color = hex(charmtone.Guppy)
		chroma.Operator.Color = hex(charmtone.Salmon)
		chroma.NameTag.Color = hex(charmtone.Mauve)
		chroma.NameAttribute.Color = hex(charmtone.Hazy)
		chroma.NameClass.Color = hex(charmtone.Salt)
		chroma.LiteralString.Color = hex(charmtone.Cumin)
	}
	s.Markdown.Link.Color = hex(charmtone.Zinc)
	s.Markdown.Image.Color = hex(charmtone.Cheeky)

	// The ◆ hypercredit symbol inside subdued text (e.g. savings
	// suffixes) uses Mochi so it stays visible against its surroundings.
	s.Messages.SubduedHypercreditIcon = s.Messages.SubduedHypercreditIcon.
		Foreground(charmtone.Violet)

	return s
}

// HypercrushObsidiana returns the Hypercrush dark theme.
func HypercrushObsidiana() Styles {
	return CharmtonePantera()
}

// gruvboxDarkOpts returns the quickStyleOpts for the Gruvbox Dark theme,
// using canonical colors from the morhetz/gruvbox palette.
func gruvboxDarkOpts() quickStyleOpts {
	return quickStyleOpts{
		primary:   lipgloss.Color("#fabd2f"), // yellow
		secondary: lipgloss.Color("#d3869b"), // purple
		accent:    lipgloss.Color("#b8bb26"), // green
		keyword:   lipgloss.Color("#fe8019"), // orange

		fgBase:       lipgloss.Color("#ebdbb2"), // fg
		fgMoreSubtle: lipgloss.Color("#a89984"), // fg4/gray
		fgSubtle:     lipgloss.Color("#bdae93"), // fg3
		fgMostSubtle: lipgloss.Color("#928374"), // gray

		onPrimary: lipgloss.Color("#282828"), // bg on primary

		bgBase:         lipgloss.Color("#282828"), // bg
		bgLeastVisible: lipgloss.Color("#3c3836"), // bg1
		bgLessVisible:  lipgloss.Color("#504945"), // bg2
		bgMostVisible:  lipgloss.Color("#665c54"), // bg3

		separator: lipgloss.Color("#504945"), // bg2

		destructive:       lipgloss.Color("#fb4934"), // red bright
		error:             lipgloss.Color("#cc241d"), // red dark
		warningSubtle:     lipgloss.Color("#fabd2f"), // yellow bright
		warning:           lipgloss.Color("#d79921"), // yellow dark
		attention:         lipgloss.Color("#fe8019"), // orange
		busy:              lipgloss.Color("#fabd2f"), // yellow bright
		info:              lipgloss.Color("#83a598"), // blue bright
		infoMoreSubtle:    lipgloss.Color("#83a598"), // blue bright
		infoMostSubtle:    lipgloss.Color("#458588"), // blue dark
		success:           lipgloss.Color("#b8bb26"), // green bright
		successMoreSubtle: lipgloss.Color("#b8bb26"), // green bright
		successMostSubtle: lipgloss.Color("#8ec07c"), // aqua bright

		yolo:           lipgloss.Color("#fabd2f"), // yellow bright
		plan:           lipgloss.Color("#d3869b"), // purple
		planMoreSubtle: lipgloss.Color("#665c54"), // bg3

		// Diff colors derive from success/destructive over bgBase.

		button:         lipgloss.Color("#d3869b"), // purple
		buttonSubtle:   lipgloss.Color("#504945"), // bg2
		buttonInactive: lipgloss.Color("#665c54"), // bg3
		buttonHovered:  lipgloss.Color("#928374"), // gray

		// ANSI 16-color palette for remapping raw terminal output
		// (e.g. bang-mode shell commands) onto legible Gruvbox colors.
		ansiBlack:   lipgloss.Color("#282828"),
		ansiRed:     lipgloss.Color("#cc241d"),
		ansiGreen:   lipgloss.Color("#98971a"),
		ansiYellow:  lipgloss.Color("#d79921"),
		ansiBlue:    lipgloss.Color("#458588"),
		ansiMagenta: lipgloss.Color("#b16286"),
		ansiCyan:    lipgloss.Color("#689d6a"),
		ansiWhite:   lipgloss.Color("#a89984"),

		ansiBrightBlack:   lipgloss.Color("#928374"),
		ansiBrightRed:     lipgloss.Color("#fb4934"),
		ansiBrightGreen:   lipgloss.Color("#b8bb26"),
		ansiBrightYellow:  lipgloss.Color("#fabd2f"),
		ansiBrightBlue:    lipgloss.Color("#83a598"),
		ansiBrightMagenta: lipgloss.Color("#d3869b"),
		ansiBrightCyan:    lipgloss.Color("#8ec07c"),
		ansiBrightWhite:   lipgloss.Color("#ebdbb2"),
	}
}

// gruvboxDarkOverrides applies Gruvbox-specific tweaks on top of the
// token-driven base styles.
func gruvboxDarkOverrides(s Styles) Styles {
	// The shared quickStyle renders inline code in the destructive (bright
	// red) color, which in Gruvbox reads poorly against the background. Use
	// Gruvbox orange instead, which keeps a warm "code" feel.
	s.Markdown.Code.Color = hex(lipgloss.Color("#fe8019"))
	return s
}

// terminalOpts returns the quickStyleOpts for the Terminal theme: plain
// white-on-dark text with the Konsole default (Breeze) ANSI colors, so Crush
// looks like an ordinary shell session.
func terminalOpts() quickStyleOpts {
	return quickStyleOpts{
		primary:   lipgloss.Color("#11d116"), // green, like a shell prompt
		secondary: lipgloss.Color("#3daee9"), // bright blue
		accent:    lipgloss.Color("#1cdc9a"), // bright green
		keyword:   lipgloss.Color("#fdbc4b"), // bright yellow

		fgBase:       lipgloss.Color("#fcfcfc"),
		fgMoreSubtle: lipgloss.Color("#bdc3c7"),
		fgSubtle:     lipgloss.Color("#dcdfe1"),
		fgMostSubtle: lipgloss.Color("#7f8c8d"),

		onPrimary: lipgloss.Color("#232627"),

		bgBase:         lipgloss.Color("#232627"),
		bgLeastVisible: lipgloss.Color("#2c3032"),
		bgLessVisible:  lipgloss.Color("#383c3e"),
		bgMostVisible:  lipgloss.Color("#4d5254"),

		separator: lipgloss.Color("#383c3e"),

		destructive:       lipgloss.Color("#ed1515"),
		error:             lipgloss.Color("#c0392b"),
		warningSubtle:     lipgloss.Color("#fdbc4b"),
		warning:           lipgloss.Color("#f67400"),
		attention:         lipgloss.Color("#f67400"),
		busy:              lipgloss.Color("#fdbc4b"),
		info:              lipgloss.Color("#1d99f3"),
		infoMoreSubtle:    lipgloss.Color("#3daee9"),
		infoMostSubtle:    lipgloss.Color("#1b668f"),
		success:           lipgloss.Color("#11d116"),
		successMoreSubtle: lipgloss.Color("#1cdc9a"),
		successMostSubtle: lipgloss.Color("#16a085"),
		yolo:              lipgloss.Color("#fdbc4b"),
		plan:              lipgloss.Color("#1abc9c"),
		planMoreSubtle:    lipgloss.Color("#4d5254"),

		button:         lipgloss.Color("#1d99f3"),
		buttonSubtle:   lipgloss.Color("#383c3e"),
		buttonInactive: lipgloss.Color("#4d5254"),
		buttonHovered:  lipgloss.Color("#7f8c8d"),

		ansiBlack:   lipgloss.Color("#232627"),
		ansiRed:     lipgloss.Color("#ed1515"),
		ansiGreen:   lipgloss.Color("#11d116"),
		ansiYellow:  lipgloss.Color("#f67400"),
		ansiBlue:    lipgloss.Color("#1d99f3"),
		ansiMagenta: lipgloss.Color("#9b59b6"),
		ansiCyan:    lipgloss.Color("#1abc9c"),
		ansiWhite:   lipgloss.Color("#fcfcfc"),

		ansiBrightBlack:   lipgloss.Color("#7f8c8d"),
		ansiBrightRed:     lipgloss.Color("#c0392b"),
		ansiBrightGreen:   lipgloss.Color("#1cdc9a"),
		ansiBrightYellow:  lipgloss.Color("#fdbc4b"),
		ansiBrightBlue:    lipgloss.Color("#3daee9"),
		ansiBrightMagenta: lipgloss.Color("#8e44ad"),
		ansiBrightCyan:    lipgloss.Color("#16a085"),
		ansiBrightWhite:   lipgloss.Color("#ffffff"),
	}
}

// claudeCodeOpts returns the quickStyleOpts for the Claude Code theme, using
// the colors of Claude Code's dark theme: Claude orange, lavender prompts,
// teal plan mode and muted grays.
func claudeCodeOpts() quickStyleOpts {
	return quickStyleOpts{
		primary:   lipgloss.Color("#d77757"), // claude orange
		secondary: lipgloss.Color("#eb9f7f"), // claude shimmer
		accent:    lipgloss.Color("#b1b9f9"), // suggestion lavender
		keyword:   lipgloss.Color("#af87ff"), // auto-accept purple

		fgBase:       lipgloss.Color("#ffffff"),
		fgMoreSubtle: lipgloss.Color("#999999"),
		fgSubtle:     lipgloss.Color("#c8c8c8"),
		fgMostSubtle: lipgloss.Color("#808080"),

		onPrimary: lipgloss.Color("#1f1f1e"),

		bgBase:         lipgloss.Color("#1f1f1e"),
		bgLeastVisible: lipgloss.Color("#2a2a28"),
		bgLessVisible:  lipgloss.Color("#373734"),
		bgMostVisible:  lipgloss.Color("#505050"),

		separator: lipgloss.Color("#505050"),

		destructive:       lipgloss.Color("#ff6b80"),
		error:             lipgloss.Color("#ff6b80"),
		warningSubtle:     lipgloss.Color("#ffd75f"),
		warning:           lipgloss.Color("#ffc107"),
		attention:         lipgloss.Color("#d77757"),
		busy:              lipgloss.Color("#ffc107"),
		info:              lipgloss.Color("#b1b9f9"),
		infoMoreSubtle:    lipgloss.Color("#b1b9f9"),
		infoMostSubtle:    lipgloss.Color("#5769f7"),
		success:           lipgloss.Color("#4eba65"),
		successMoreSubtle: lipgloss.Color("#6fd087"),
		successMostSubtle: lipgloss.Color("#38a660"),
		yolo:              lipgloss.Color("#ff6b80"),
		plan:              lipgloss.Color("#48968c"),
		planMoreSubtle:    lipgloss.Color("#2f5f59"),

		// Claude Code's diff greens and reds.
		diffInsertFg:       lipgloss.Color("#38a660"),
		diffInsertCodeBg:   lipgloss.Color("#225c2b"),
		diffInsertGutterBg: lipgloss.Color("#1a4420"),
		diffDeleteFg:       lipgloss.Color("#b3596b"),
		diffDeleteCodeBg:   lipgloss.Color("#7a2936"),
		diffDeleteGutterBg: lipgloss.Color("#5a1f29"),

		button:         lipgloss.Color("#d77757"),
		buttonSubtle:   lipgloss.Color("#373734"),
		buttonInactive: lipgloss.Color("#505050"),
		buttonHovered:  lipgloss.Color("#808080"),

		ansiBlack:   lipgloss.Color("#1f1f1e"),
		ansiRed:     lipgloss.Color("#ff6b80"),
		ansiGreen:   lipgloss.Color("#4eba65"),
		ansiYellow:  lipgloss.Color("#ffc107"),
		ansiBlue:    lipgloss.Color("#5769f7"),
		ansiMagenta: lipgloss.Color("#fd5db1"),
		ansiCyan:    lipgloss.Color("#48968c"),
		ansiWhite:   lipgloss.Color("#c8c8c8"),

		ansiBrightBlack:   lipgloss.Color("#808080"),
		ansiBrightRed:     lipgloss.Color("#ff8a9a"),
		ansiBrightGreen:   lipgloss.Color("#6fd087"),
		ansiBrightYellow:  lipgloss.Color("#ffd75f"),
		ansiBrightBlue:    lipgloss.Color("#b1b9f9"),
		ansiBrightMagenta: lipgloss.Color("#af87ff"),
		ansiBrightCyan:    lipgloss.Color("#6cc5b8"),
		ansiBrightWhite:   lipgloss.Color("#ffffff"),
	}
}

// graphiteOpts returns the Graphite theme: quiet gray surfaces with one
// ice-blue accent. Green stays a small success mark only.
func graphiteOpts() quickStyleOpts {
	// High contrast, like Codex: near-white text, a light gray for
	// secondary text, bands that stand clear of the background, and
	// saturated accents.
	return quickStyleOpts{
		primary:   lipgloss.Color("#5fb3ef"), // clear sky blue
		secondary: lipgloss.Color("#9ccff5"), // light sky blue
		accent:    lipgloss.Color("#5fb3ef"),
		keyword:   lipgloss.Color("#b48ce6"), // violet

		fgBase:       lipgloss.Color("#ffffff"), // pure white: text stands out
		fgSubtle:     lipgloss.Color("#d9dbde"),
		fgMoreSubtle: lipgloss.Color("#a8abb0"),
		fgMostSubtle: lipgloss.Color("#868a91"),

		onPrimary: lipgloss.Color("#1e1f22"),

		bgBase:         lipgloss.Color("#1e1f22"),
		bgLeastVisible: lipgloss.Color("#42454a"),
		bgLessVisible:  lipgloss.Color("#4b4e54"),
		bgMostVisible:  lipgloss.Color("#5d6168"),

		separator: lipgloss.Color("#4d5057"),

		destructive:       lipgloss.Color("#f08ca6"),
		error:             lipgloss.Color("#f08ca6"),
		warningSubtle:     lipgloss.Color("#f5b98c"),
		warning:           lipgloss.Color("#f0c674"),
		attention:         lipgloss.Color("#5fb3ef"),
		busy:              lipgloss.Color("#f0c674"),
		info:              lipgloss.Color("#5fb3ef"),
		infoMoreSubtle:    lipgloss.Color("#7fc2f0"),
		infoMostSubtle:    lipgloss.Color("#4a86b3"),
		success:           lipgloss.Color("#7fd197"),
		successMoreSubtle: lipgloss.Color("#9adcad"),
		successMostSubtle: lipgloss.Color("#5f9e72"),
		yolo:              lipgloss.Color("#f0c674"),
		plan:              lipgloss.Color("#b48ce6"),
		planMoreSubtle:    lipgloss.Color("#544a72"),

		diffInsertFg:       lipgloss.Color("#7fd197"),
		diffInsertCodeBg:   lipgloss.Color("#26402e"),
		diffInsertGutterBg: lipgloss.Color("#203527"),
		diffDeleteFg:       lipgloss.Color("#f08ca6"),
		diffDeleteCodeBg:   lipgloss.Color("#4a2830"),
		diffDeleteGutterBg: lipgloss.Color("#3c2128"),

		button:         lipgloss.Color("#5fb3ef"),
		buttonSubtle:   lipgloss.Color("#44474d"),
		buttonInactive: lipgloss.Color("#575b62"),
		buttonHovered:  lipgloss.Color("#868a91"),

		ansiBlack:   lipgloss.Color("#1e1f22"),
		ansiRed:     lipgloss.Color("#f08ca6"),
		ansiGreen:   lipgloss.Color("#7fd197"),
		ansiYellow:  lipgloss.Color("#f0c674"),
		ansiBlue:    lipgloss.Color("#5fb3ef"),
		ansiMagenta: lipgloss.Color("#b48ce6"),
		ansiCyan:    lipgloss.Color("#5fd0d0"),
		ansiWhite:   lipgloss.Color("#d9dbde"),

		ansiBrightBlack:   lipgloss.Color("#868a91"),
		ansiBrightRed:     lipgloss.Color("#f7aabd"),
		ansiBrightGreen:   lipgloss.Color("#9adcad"),
		ansiBrightYellow:  lipgloss.Color("#f6d68f"),
		ansiBrightBlue:    lipgloss.Color("#9ccff5"),
		ansiBrightMagenta: lipgloss.Color("#cbaef0"),
		ansiBrightCyan:    lipgloss.Color("#8fe0e0"),
		ansiBrightWhite:   lipgloss.Color("#ffffff"),
	}
}

// builtinThemes maps theme names to their quickStyleOpts palette definitions.
var builtinThemes = map[string]func() quickStyleOpts{
	"charmtone-panther": charmtoneOpts,
	"claude-code":       claudeCodeOpts,
	"graphite":          graphiteOpts,
	"gruvbox-dark":      gruvboxDarkOpts,
	"terminal":          terminalOpts,
}

// builtinThemeOverrides maps theme names to functions that apply
// theme-specific style tweaks on top of the styles produced by
// [quickStyle]. Themes without overrides are absent from the map.
var builtinThemeOverrides = map[string]func(Styles) Styles{
	"charmtone-panther": charmtoneOverrides,
	"gruvbox-dark":      gruvboxDarkOverrides,
}

// deprecatedThemeNames maps legacy built-in theme names to their current
// names so existing configs and user theme files keep resolving.
var deprecatedThemeNames = map[string]string{
	"charmtone": "charmtone-panther",
}

// normalizeThemeName lowercases a theme name and maps deprecated built-in
// names to their current equivalents.
func normalizeThemeName(name string) string {
	key := strings.ToLower(name)
	if renamed, ok := deprecatedThemeNames[key]; ok {
		return renamed
	}
	return key
}

// BuiltinThemeNames returns the names of all built-in themes, sorted.
func BuiltinThemeNames() []string {
	names := make([]string, 0, len(builtinThemes))
	for name := range builtinThemes {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// LoadTheme loads a theme by name. User theme files (in ThemeDirs) take
// precedence over built-in themes of the same name. Returns
// CharmtonePantera styles for an empty name. Returns an error if the
// name is not recognized as either a user file or a built-in.
// LoadTheme loads a theme by name. Global user theme files take precedence
// over built-in themes of the same name. Empty names use Charmtone.
func LoadTheme(name string) (Styles, error) {
	if name == "" {
		return CharmtonePantera(), nil
	}
	key := normalizeThemeName(name)

	if path, err := FindThemeFile(key); err == nil {
		tf, err := LoadThemeFile(path)
		if err != nil {
			return Styles{}, err
		}
		return LoadPaletteTheme(tf.Base, tf.Palette)
	}

	optsFn, ok := builtinThemes[key]
	if !ok {
		return Styles{}, fmt.Errorf("unknown theme %q; available themes: %s", name, strings.Join(BuiltinThemeNames(), ", "))
	}
	s := quickStyle(optsFn())
	if override, ok := builtinThemeOverrides[key]; ok {
		s = override(s)
	}
	return s, nil
}

// ThemeFromConfig resolves the configured theme name, falling back to the
// default Charmtone theme when the config value is empty or invalid. The
// fallback is logged so a missing or broken theme file is not silently
// replaced by different colors.
func ThemeFromConfig(name string) Styles {
	s, err := LoadTheme(name)
	if err != nil {
		slog.Warn("Falling back to the default theme", "configured_theme", name, "reason", err)
		return CharmtonePantera()
	}
	return s
}

// ThemeSource indicates where a theme definition comes from.
type ThemeSource int

const (
	// ThemeSourceBuiltin is a theme compiled into the binary.
	ThemeSourceBuiltin ThemeSource = iota
	// ThemeSourceUser is a theme file in ~/.config/crush/themes/.
	ThemeSourceUser
)

// String returns a human-readable label for the theme source.
func (s ThemeSource) String() string {
	if s == ThemeSourceUser {
		return "user"
	}
	return "builtin"
}

// ThemeInfo describes an available theme for listing purposes.
type ThemeInfo struct {
	Name       string
	Source     ThemeSource
	Overridden bool // true if a user file shadows a builtin
}

// ListAllThemes returns all available themes (built-in + global user files),
// sorted by name. Built-in themes shadowed by a user file are marked as
// Overridden.
func ListAllThemes() []ThemeInfo {
	userThemes, _ := ListUserThemes()
	userSet := make(map[string]bool, len(userThemes))
	for _, n := range userThemes {
		userSet[n] = true
	}

	// Determine which user themes shadow builtins.
	overridden := make(map[string]bool)
	for _, n := range userThemes {
		if _, ok := builtinThemes[n]; ok {
			overridden[n] = true
		}
	}

	var infos []ThemeInfo

	// Add built-in themes (mark overridden ones).
	for _, name := range BuiltinThemeNames() {
		infos = append(infos, ThemeInfo{
			Name:       name,
			Source:     ThemeSourceBuiltin,
			Overridden: overridden[name],
		})
	}

	// Add user themes that don't shadow a builtin.
	for _, name := range userThemes {
		if _, isBuiltin := builtinThemes[name]; isBuiltin {
			continue
		}
		infos = append(infos, ThemeInfo{
			Name:   name,
			Source: ThemeSourceUser,
		})
	}

	sort.Slice(infos, func(i, j int) bool {
		return infos[i].Name < infos[j].Name
	})
	return infos
}

// ExportResolvedPalette resolves a theme fully (all palette tokens
// filled) and returns it as a ThemeFile suitable for writing to disk.
// This is used when forking a built-in theme or exporting the current
// palette. The returned ThemeFile has Base set to the source theme name
// and all Palette fields populated with resolved color values.
// ExportResolvedPalette resolves a theme fully and returns it as a ThemeFile
// suitable for writing to disk.
func ExportResolvedPalette(name string) (*ThemeFile, error) {
	key := strings.ToLower(name)
	palette, root, err := resolveThemePalette(key, map[string]bool{})
	if err != nil {
		return nil, err
	}
	rootOpts, err := builtinThemeOpts(root)
	if err != nil {
		return nil, err
	}
	opts := palette.ToQuickStyleOpts(rootOpts)
	opts.deriveDiffColors()
	return &ThemeFile{Base: root, Palette: PaletteFromOpts(opts)}, nil
}

// IsBuiltinTheme reports whether the given name matches a built-in theme.
// Deprecated names still count as built-in.
func IsBuiltinTheme(name string) bool {
	_, ok := builtinThemes[normalizeThemeName(name)]
	return ok
}
