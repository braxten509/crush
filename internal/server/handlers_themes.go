package server

import (
	"net/http"

	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/rpc"
)

// Handlers of the theme routes (desktop design 5-6, 3.2; G-2).

func (c *controllerV1) listThemes(*http.Request, rpc.Empty) (proto.ThemeList, error) {
	return c.backend.ListThemes(), nil
}

func (c *controllerV1) getTheme(r *http.Request, _ rpc.Empty) (proto.ThemeView, error) {
	return c.backend.Theme(r.PathValue("name"))
}

func (c *controllerV1) resolveTheme(_ *http.Request, req proto.ThemePaletteRequest) (proto.ThemeView, error) {
	return c.backend.ResolveTheme(req)
}

func (c *controllerV1) saveTheme(r *http.Request, req proto.ThemePaletteRequest) (proto.ThemeView, error) {
	return c.backend.SaveTheme(r.PathValue("name"), req)
}

func (c *controllerV1) createTheme(_ *http.Request, req proto.ThemeCreateRequest) (proto.ThemeView, error) {
	return c.backend.CreateTheme(req)
}

func (c *controllerV1) renameTheme(r *http.Request, req proto.ThemeRenameRequest) (rpc.Empty, error) {
	return rpc.Empty{}, c.backend.RenameTheme(r.PathValue("name"), req.NewName)
}

func (c *controllerV1) deleteTheme(r *http.Request, _ rpc.Empty) (rpc.Empty, error) {
	return rpc.Empty{}, c.backend.DeleteTheme(r.PathValue("name"))
}

func (c *controllerV1) revertTheme(r *http.Request, _ rpc.Empty) (rpc.Empty, error) {
	return rpc.Empty{}, c.backend.RevertTheme(r.PathValue("name"))
}
