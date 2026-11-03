package server

import (
	"github.com/charmbracelet/crush/internal/localmodel"
	"github.com/charmbracelet/crush/internal/rpc"
	"net/http"
)

func (c *controllerV1) localModels(_ *http.Request, _ rpc.Empty) (localmodel.Status, error) {
	return c.backend.LocalModels(), nil
}
func (c *controllerV1) loadLocalModel(_ *http.Request, req rpc.LocalModelRequest) (localmodel.Status, error) {
	if req.ModelID == "" {
		return localmodel.Status{}, badRequest("model_id is required")
	}
	status, err := c.backend.LoadLocalModel(req.ModelID)
	if err != nil {
		return status, badRequest(err.Error())
	}
	return status, nil
}
func (c *controllerV1) unloadLocalModel(_ *http.Request, _ rpc.Empty) (localmodel.Status, error) {
	return c.backend.UnloadLocalModel()
}
