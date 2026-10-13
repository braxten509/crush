package server

import (
	"encoding/json"
	"net/http"

	"github.com/charmbracelet/crush/internal/session"
)

func (c *controllerV1) handleSavedSessions(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if r.Method == http.MethodGet {
		entries, err := c.backend.ListSavedSessions(r.Context(), id, r.URL.Query().Get("selected"))
		if err != nil {
			c.handleError(w, r, err)
			return
		}
		jsonEncode(w, entries)
		return
	}
	if _, err := c.backend.GetWorkspace(id); err != nil {
		c.handleError(w, r, err)
		return
	}
	var change session.CatalogChange
	if err := json.NewDecoder(r.Body).Decode(&change); err != nil {
		http.Error(w, "invalid saved chat", http.StatusBadRequest)
		return
	}
	if err := c.backend.ChangeSavedSession(r.Context(), id, change); err != nil {
		c.handleError(w, r, err)
		return
	}
	jsonEncode(w, struct{}{})
}
