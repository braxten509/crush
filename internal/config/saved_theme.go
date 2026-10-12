package config

import "github.com/tidwall/gjson"

// SavedTheme returns the theme name saved in the global JSON configs,
// read in the same order a full load merges them, so the last one that
// sets a theme wins. It is cheap enough to call before the full config
// loads. Crushrc files are skipped because reading them runs Bash. It
// returns "" (the default theme) when no file sets one.
func SavedTheme() string {
	name := ""
	for _, path := range []string{systemConfigPath, GlobalConfig(), GlobalConfigData()} {
		if path == "" {
			continue
		}
		data, err := readFile(path)
		if err != nil {
			continue
		}
		if v := gjson.GetBytes(data, "options.tui.active_theme"); v.Type == gjson.String {
			name = v.String()
		} else if v := gjson.GetBytes(data, "options.tui.theme"); v.Type == gjson.String {
			// The legacy field held the name directly.
			name = v.String()
		}
	}
	return name
}
