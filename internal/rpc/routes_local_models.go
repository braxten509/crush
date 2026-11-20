package rpc

import "github.com/charmbracelet/crush/internal/localmodel"

type LocalModelRequest struct {
	ModelID string `json:"model_id"`
}

// LocalModelSizeRequest saves a model's context size; 0 is "biggest that fits".
type LocalModelSizeRequest struct {
	ModelID string `json:"model_id"`
	Context int64  `json:"context"`
}

var (
	LocalModels      = Op[Empty, localmodel.Status]{Method: get, Path: "/v1/local-models"}
	LoadLocalModel   = Op[LocalModelRequest, localmodel.Status]{Method: post, Path: "/v1/local-models/load"}
	UnloadLocalModel = Op[Empty, localmodel.Status]{Method: post, Path: "/v1/local-models/unload"}
	SizeLocalModel   = Op[LocalModelSizeRequest, localmodel.Status]{Method: post, Path: "/v1/local-models/size"}
)
