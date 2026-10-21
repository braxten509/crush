package rpc

import (
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/proto"
)

// Accounts and the fallback order.
var (
	GetFallback      = Op[Empty, proto.FallbackSettings]{Method: get, Path: "/v1/workspaces/{id}/fallback"}
	SetFallbackOrder = Op[config.FallbackOrder, proto.FallbackSettings]{Method: put, Path: "/v1/workspaces/{id}/fallback/order"}
	AddAccount       = Op[proto.AccountAddRequest, proto.AccountLogin]{Method: post, Path: "/v1/workspaces/{id}/accounts"}
	RenameAccount    = Op[proto.AccountRenameRequest, proto.FallbackSettings]{Method: put, Path: "/v1/workspaces/{id}/accounts/{aid}"}
	RemoveAccount    = Op[Empty, proto.FallbackSettings]{Method: del, Path: "/v1/workspaces/{id}/accounts/{aid}"}
	SignInAccount    = Op[Empty, proto.AccountLogin]{Method: post, Path: "/v1/workspaces/{id}/accounts/{aid}/login"}
	UseRefill        = Op[proto.RefillRequest, proto.RefillResult]{Method: post, Path: "/v1/limits/refill"}
)
