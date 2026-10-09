package server

import (
	"encoding/json"
	"github.com/charmbracelet/crush/internal/session"
	"net/http"
)

func (c *controllerV1) handleSavedSessions(w http.ResponseWriter, r *http.Request) {
	ws, err := c.backend.GetWorkspace(r.PathValue("id"))
	if err != nil {
		c.handleError(w, r, err)
		return
	}
	catalog := session.Catalog{Directory: ws.Path, DataDirectory: ws.Cfg.Config().Options.DataDirectory}
	if r.Method == http.MethodGet {
		entries, err := catalog.List(r.Context(), r.URL.Query().Get("selected"))
		if err != nil {
			c.handleError(w, r, err)
			return
		}
		jsonEncode(w, entries)
		return
	}
	var change session.CatalogChange
	if err := json.NewDecoder(r.Body).Decode(&change); err != nil {
		http.Error(w, "invalid saved chat", http.StatusBadRequest)
		return
	}
	if change.DiscardEmpty {
		if err := catalog.DiscardEmpty(r.Context(), change.Session.ID); err != nil {
			c.handleError(w, r, err)
			return
		}
		jsonEncode(w, struct{}{})
		return
	}
	if err := catalog.Change(r.Context(), change.Session, change.Remove); err != nil {
		c.handleError(w, r, err)
		return
	}
	jsonEncode(w, struct{}{})
}
