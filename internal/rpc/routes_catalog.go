package rpc

import "charm.land/catwalk/pkg/catwalk"

// A provider's catalog (OpenRouter, OrcaRouter), for picking the models to
// save under it.
var ProviderCatalog = Op[Empty, []catwalk.Model]{Method: get, Path: "/v1/workspaces/{id}/providers/{pid}/catalog"}
