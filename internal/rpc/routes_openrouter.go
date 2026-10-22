package rpc

import "charm.land/catwalk/pkg/catwalk"

// OpenRouter's catalog, for picking the models to save under it.
var OpenRouterCatalog = Op[Empty, []catwalk.Model]{Method: get, Path: "/v1/workspaces/{id}/openrouter/catalog"}
