package rpc

import "github.com/charmbracelet/crush/internal/proto"

// Theme routes (desktop design 5-6, 3.2; G-2). Global, like /v1/memory.
var (
	ListThemes   = Op[Empty, proto.ThemeList]{Method: get, Path: "/v1/themes"}
	GetTheme     = Op[Empty, proto.ThemeView]{Method: get, Path: "/v1/themes/{name}"}
	ResolveTheme = Op[proto.ThemePaletteRequest, proto.ThemeView]{Method: post, Path: "/v1/themes/resolve"}
	SaveTheme    = Op[proto.ThemePaletteRequest, proto.ThemeView]{Method: put, Path: "/v1/themes/{name}"}
	CreateTheme  = Op[proto.ThemeCreateRequest, proto.ThemeView]{Method: post, Path: "/v1/themes"}
	RenameTheme  = Op[proto.ThemeRenameRequest, Empty]{Method: post, Path: "/v1/themes/{name}/rename"}
	DeleteTheme  = Op[Empty, Empty]{Method: del, Path: "/v1/themes/{name}"}
	RevertTheme  = Op[Empty, Empty]{Method: post, Path: "/v1/themes/{name}/revert"}
)
