package rpc

import "github.com/charmbracelet/crush/internal/localmodel"

type LocalModelRequest struct {
	ModelID string `json:"model_id"`
}

var (
	LocalModels      = Op[Empty, localmodel.Status]{Method: get, Path: "/v1/local-models"}
	LoadLocalModel   = Op[LocalModelRequest, localmodel.Status]{Method: post, Path: "/v1/local-models/load"}
	UnloadLocalModel = Op[Empty, localmodel.Status]{Method: post, Path: "/v1/local-models/unload"}
)
