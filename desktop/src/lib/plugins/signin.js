// AUTH's registrations (desktop-design-5-6 2.4, 7.3; loaded by lib/plugins.js):
// the sheets `auth_method`, `api_key_input`, `oauth`, `aws_sso` (and
// sign-out's question), and the agent_event listener for aws_sso_auth,
// aws_sso_auth_result and re_authenticate on this window's chat.
import ApiKey from "../../components/signin/ApiKey.svelte";
import AuthMethod from "../../components/signin/AuthMethod.svelte";
import AwsSso from "../../components/signin/AwsSso.svelte";
import OAuth from "../../components/signin/OAuth.svelte";
import SignOut from "../../components/signin/SignOut.svelte";
import { closeDialog, hasDialog, openDialog, registerDialog } from "../dialogs.js";
import { onEvent } from "../engine/events.js";
import { rootOf, sessions } from "../stores/sessions.svelte.js";
import { ws } from "../stores/workspace.svelte.js";
import { awsSso, resetAwsSso } from "../signin/store.svelte.js";

registerDialog("auth_method", AuthMethod);
registerDialog("api_key_input", ApiKey);
registerDialog("oauth", OAuth);
registerDialog("aws_sso", AwsSso);
registerDialog("signout_confirm", SignOut);

// The event is for this window when it names the shown chat (or one of its helpers).
function forThisWindow(sessionId) {
  const shown = sessions.currentId;
  if (!sessionId) return true;
  return Boolean(shown) && (sessionId === shown || rootOf(sessionId) === shown);
}

// internal/ui/model/agent_events.go:171-252.
onEvent("agent_event", (_kind, event) => {
  if (!event || !forThisWindow(event.session_id)) return;
  switch (event.type) {
    case "aws_sso_auth": {
      if (hasDialog("aws_sso")) {
        if (event.aws_sso_url) awsSso.url = event.aws_sso_url;
        if (event.aws_sso_command) awsSso.command = event.aws_sso_command;
        awsSso.state = "waiting";
        return;
      }
      resetAwsSso(event.aws_sso_command ?? "", event.aws_sso_url ?? "");
      openDialog("aws_sso", {}, { grace: true });
      return;
    }
    case "aws_sso_auth_result": {
      if (!hasDialog("aws_sso")) return;
      if (event.error) {
        awsSso.state = "error";
        awsSso.error = event.error;
      } else closeDialog("aws_sso");
      return;
    }
    case "re_authenticate": {
      // G-10 names the provider; without it, the coder model's provider.
      const providerId = event.provider_id || ws.agent?.model_cfg?.provider || "";
      if (!providerId) return;
      openDialog("auth_method", { providerId, reAuthenticate: true, onDone: () => {} }, { grace: true });
      return;
    }
    default:
  }
});
