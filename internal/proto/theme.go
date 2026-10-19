package proto

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
