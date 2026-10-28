// Which agent CLIs hold their own login (GET auth/state, backend.AuthState):
// Settings → Accounts' detail panels read it, and read it again when the
// config changes or a sign-in terminal closes. Booleans and, where a CLI
// keeps one, its login's email cross the wire; no credential is read here.
import { api } from "../engine/api.js";
import { onEvent } from "../engine/events.js";

export const cliState = $state({
  loaded: false,
  providers: {}, // provider id → signed in
  emails: {}, // provider id → the email its login uses, when known
});

export async function loadCLIState() {
  try {
    const answer = await api.authState();
    cliState.providers = answer?.providers ?? {};
    cliState.emails = answer?.emails ?? {};
    cliState.loaded = true;
  } catch {
    // The panel still offers the sign-in; only the state line is missing.
  }
}

onEvent("config_changed", () => {
  if (cliState.loaded) loadCLIState();
});
