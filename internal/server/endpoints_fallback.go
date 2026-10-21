package server

import (
	"errors"
	"net/http"

	"github.com/charmbracelet/crush/internal/apigen"
	"github.com/charmbracelet/crush/internal/backend"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/rpc"
)

// fallbackEndpoints documents and routes the accounts and the fallback
// order.
func (c *controllerV1) fallbackEndpoints() []apigen.Endpoint {
	return []apigen.Endpoint{
		route(c, rpc.GetFallback, func(r *http.Request, _ rpc.Empty) (proto.FallbackSettings, error) {
			return c.backend.FallbackSettings(r.PathValue("id"))
		}).
			Summary("Get the accounts and the fallback order").
			Description("Lists each Claude Code and Codex account, the tool's own login (ID \"\") first, "+
				"and the order chats move through when an account runs out.").
			Tags("fallback").
			PathParam("id", "Workspace ID").
			Fails(404, 500).
			Build(),

		route(c, rpc.SetFallbackOrder, func(r *http.Request, order config.FallbackOrder) (proto.FallbackSettings, error) {
			return c.backend.SetFallbackOrder(r.PathValue("id"), order)
		}).
			Summary("Save the fallback order").
			Description("Checks and saves the order in the global config. A block with an unknown "+
				"provider, model or account answers 400 with the reason.").
			Tags("fallback").
			PathParam("id", "Workspace ID").
			Fails(400, 404, 500).
			Build(),

		route(c, rpc.AddAccount, func(r *http.Request, req proto.AccountAddRequest) (proto.AccountLogin, error) {
			return c.backend.AddAccount(r.PathValue("id"), req.Kind)
		}).
			Summary("Add a Claude Code or Codex account").
			Description("Makes the account's folder, with the tool's own settings, skills, plugins, "+
				"MCP tools and saved sessions linked in, and returns the command that signs it in.").
			Tags("fallback").
			PathParam("id", "Workspace ID").
			Fails(400, 404, 500).
			Build(),

		route(c, rpc.RenameAccount, func(r *http.Request, req proto.AccountRenameRequest) (proto.FallbackSettings, error) {
			return c.backend.RenameAccount(r.PathValue("id"), r.PathValue("aid"), req.Name)
		}).
			Summary("Rename an account").
			Tags("fallback").
			PathParam("id", "Workspace ID").
			PathParam("aid", "Account ID").
			Fails(404, 500).
			Build(),

		route(c, rpc.RemoveAccount, func(r *http.Request, _ rpc.Empty) (proto.FallbackSettings, error) {
			return c.backend.RemoveAccount(r.PathValue("id"), r.PathValue("aid"))
		}).
			Summary("Remove an account").
			Description("Deletes the account's folder (its login and links; what they link to stays), "+
				"its entry and the order's blocks on it.").
			Tags("fallback").
			PathParam("id", "Workspace ID").
			PathParam("aid", "Account ID").
			Fails(404, 500).
			Build(),

		route(c, rpc.SignInAccount, func(r *http.Request, _ rpc.Empty) (proto.AccountLogin, error) {
			return c.backend.SignInAccount(r.PathValue("id"), r.PathValue("aid"))
		}).
			Summary("Get the command that signs an account in again").
			Tags("fallback").
			PathParam("id", "Workspace ID").
			PathParam("aid", "Account ID").
			Fails(404, 500).
			Build(),

		route(c, rpc.UseRefill, func(r *http.Request, req proto.RefillRequest) (proto.RefillResult, error) {
			return c.backend.UseRefill(r.Context(), req.Kind, req.Account)
		}).
			Summary("Use one of a Codex account's free refills").
			Description("Spends one rate-limit reset credit, then reads the account's limits again "+
				"(a limits event follows). outcome is Codex's: reset, nothingToReset, noCredit or "+
				"alreadyRedeemed.").
			Tags("fallback").
			Fails(400, 502).
			Build(),
	}
}

// fallbackErrors maps the accounts' and the order's errors.
func fallbackErrors() []apiError {
	is := func(target error) func(error) bool {
		return func(err error) bool { return errors.Is(err, target) }
	}
	return []apiError{
		{Match: is(backend.ErrAccountNotFound), Status: http.StatusNotFound, Code: "account_not_found"},
		{Match: is(backend.ErrFallbackInvalid), Status: http.StatusBadRequest, Code: "bad_request"},
	}
}
