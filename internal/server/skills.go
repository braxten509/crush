package server

import (
	"encoding/json"
	"github.com/charmbracelet/crush/internal/proto"
	"net/http"
)

func (c *controllerV1) handleGetInstalledSkills(w http.ResponseWriter, r *http.Request) {
	entries, err := c.backend.InstalledSkills(r.PathValue("id"))
	if err != nil {
		c.handleError(w, r, err)
		return
	}
	jsonEncode(w, entries)
}
func (c *controllerV1) handleManageSkill(w http.ResponseWriter, r *http.Request) {
	var req proto.ManageSkillRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid skill request")
		return
	}
	if err := c.backend.ManageSkill(r.Context(), r.PathValue("id"), req); err != nil {
		c.handleError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusOK)
}
