package server

import (
	"net/http"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/apigen"
	"github.com/charmbracelet/crush/internal/rpc"
)

// openRouterEndpoints documents and routes OpenRouter's catalog.
func (c *controllerV1) openRouterEndpoints() []apigen.Endpoint {
	return []apigen.Endpoint{
		route(c, rpc.OpenRouterCatalog, func(r *http.Request, _ rpc.Empty) ([]catwalk.Model, error) {
			return c.backend.OpenRouterCatalog(r.Context(), r.PathValue("id"))
		}).
			Summary("List OpenRouter's models").
			Description("Reads the configured OpenRouter account's model list, with its provider and "+
				"privacy preferences applied, and returns the models that can call tools as config "+
				"entries ready to save under providers.openrouter.models.").
			Tags("config").
			PathParam("id", "Workspace ID").
			Fails(404, 500).
			Build(),
	}
}
