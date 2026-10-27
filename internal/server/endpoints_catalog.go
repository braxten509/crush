package server

import (
	"net/http"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/apigen"
	"github.com/charmbracelet/crush/internal/rpc"
)

// catalogEndpoints documents and routes a provider's catalog.
func (c *controllerV1) catalogEndpoints() []apigen.Endpoint {
	return []apigen.Endpoint{
		route(c, rpc.ProviderCatalog, func(r *http.Request, _ rpc.Empty) ([]catwalk.Model, error) {
			return c.backend.ProviderCatalog(r.Context(), r.PathValue("id"), r.PathValue("pid"))
		}).
			Summary("List a provider's catalog").
			Description("Reads the model list of a configured OpenRouter or OrcaRouter account (an "+
				"OpenRouter account's provider and privacy preferences applied) and returns the "+
				"models that can call tools as config entries ready to save under "+
				"providers.<pid>.models.").
			Tags("config").
			PathParam("id", "Workspace ID").
			PathParam("pid", "Provider ID").
			Fails(404, 500).
			Build(),
	}
}
