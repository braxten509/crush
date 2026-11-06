package proto

import "maps"

// Theme routes (desktop design 5-6, 3.2; G-2).

// ThemeListItem is one theme in the list both apps show.
type ThemeListItem struct {
	Name       string `json:"name"`
	Source     string `json:"source"` // "builtin" or "user"
	Overridden bool   `json:"overridden"`
	Dark       bool   `json:"dark"`
}

// ThemeList is the answer of GET /v1/themes.
type ThemeList struct {
	Themes []ThemeListItem `json:"themes"`
}

// ThemeView is one theme, resolved for an app to draw with.
type ThemeView struct {
	// Name is the theme's name; empty for a resolved preview.
	Name string `json:"name,omitempty"`
	// Root is the built-in the theme's base chain ends at.
	Root string `json:"root"`
	Base string `json:"base"`
	// Source is "builtin" or "user"; empty for a resolved preview.
	Source     string `json:"source"`
	Overridden bool   `json:"overridden"`
	// Pure is true for a built-in no user file shadows.
	Pure bool `json:"pure"`
	Dark bool `json:"dark"`
	// Revision is the theme file's size and modification time; "" for a
	// built-in without a file.
	Revision string `json:"revision"`
	// Palette holds the 55 roles as the theme editor starts them, with
	// Charmtone names kept as names.
	Palette map[string]string `json:"palette"`
	// Resolved holds the same roles as #rrggbb.
	Resolved map[string]string `json:"resolved"`
	// Syntax and Markdown are the resolved code and Markdown colors.
	Syntax   map[string]string `json:"syntax"`
	Markdown map[string]string `json:"markdown"`
}

// ThemePaletteRequest is a theme's base and palette roles, for a preview
// or a save.
type ThemePaletteRequest struct {
	Base    string            `json:"base"`
	Palette map[string]string `json:"palette"`
}

// ThemeCreateRequest creates a theme from a base's resolved palette.
type ThemeCreateRequest struct {
	Name string `json:"name"`
	Base string `json:"base"`
}

// ThemeRenameRequest renames a user theme.
type ThemeRenameRequest struct {
	NewName string `json:"new_name"`
}

// HostTheme is a theme supplied by a host application that runs Crush in
// one of its panes (an editor, for example). It is written in Crush's own
// vocabulary, so any host can send one: Palette keys are theme-file keys
// (see styles.PaletteFields) and Syntax keys name code highlighting tokens
// (see styles.SyntaxFields). Colors the host leaves out come from Base.
// Unknown keys are ignored, so a host can target newer Crush versions.
//
// A host theme is never persisted: it lives in the server's memory for as
// long as the workspace does, or until the host clears it.
type HostTheme struct {
	// Name is the host's name for the theme. It is informational only.
	Name string `json:"name,omitempty"`
	// Source names the host application. It is informational only.
	Source string `json:"source,omitempty"`
	// Base is the Crush built-in or user theme that supplies every color
	// the host leaves out. Empty means the built-in charmtone-panther.
	Base string `json:"base,omitempty"`
	// Palette maps theme-file keys (such as "primary" or "bg_base") to
	// colors: "#rgb", "#rrggbb", an ANSI index (0-255) or a CharmTone name.
	Palette map[string]string `json:"palette,omitempty"`
	// Syntax maps code highlighting token keys (such as "keyword" or
	// "literal_string") to foreground colors, in the same formats.
	Syntax map[string]string `json:"syntax,omitempty"`
	// Transparent, when set, overrides Crush's own options.tui.transparent
	// while the host theme is applied. Nil keeps Crush's setting.
	Transparent *bool `json:"transparent,omitempty"`
}

// Equal reports whether t and o describe the same theme. Two nil themes
// are equal, and a nil map equals an empty one.
func (t *HostTheme) Equal(o *HostTheme) bool {
	if t == nil || o == nil {
		return t == o
	}
	if (t.Transparent == nil) != (o.Transparent == nil) ||
		(t.Transparent != nil && *t.Transparent != *o.Transparent) {
		return false
	}
	return t.Name == o.Name &&
		t.Source == o.Source &&
		t.Base == o.Base &&
		maps.Equal(t.Palette, o.Palette) &&
		maps.Equal(t.Syntax, o.Syntax)
}

// Clone returns a deep copy of t, or nil when t is nil.
func (t *HostTheme) Clone() *HostTheme {
	if t == nil {
		return nil
	}
	c := *t
	c.Palette = maps.Clone(t.Palette)
	c.Syntax = maps.Clone(t.Syntax)
	if t.Transparent != nil {
		c.Transparent = new(*t.Transparent)
	}
	return &c
}

// HostThemeRequest is the body of POST /v1/workspaces/{id}/theme. A nil
// Theme clears the host theme.
type HostThemeRequest struct {
	Theme *HostTheme `json:"theme"`
}

// HostThemeState is a workspace's host theme. It is the response of the
// theme endpoints and the payload of host_theme events (an updated event
// when a theme is set, a deleted one when it is cleared). A nil Theme
// means no host theme is set.
type HostThemeState struct {
	WorkspaceID string     `json:"workspace_id"`
	Theme       *HostTheme `json:"theme"`
}
