// The accounts and the fallback order as the engine holds them (GET
// fallback, internal/backend/fallback.go), read when Settings shows them and
// again on config_changed, so a change made elsewhere shows here too. Every
// change is saved at once; the engine's answer, when there is one, replaces
// what is shown.
import { api } from "../engine/api.js";
import { onEvent } from "../engine/events.js";
import { notify } from "../stores/ui.svelte.js";
import { openTerminal, showTerminal } from "../terminal/tabs.svelte.js";

export const fallback = $state({
  loaded: false,
  accounts: [], // proto.AccountInfo[]: each tool's own login (id "") first
  order: { on: false, blocks: [] },
  error: "",
});

function take(settings) {
  if (!settings || typeof settings !== "object") return;
  fallback.accounts = Array.isArray(settings.accounts) ? settings.accounts : [];
  fallback.order = { on: Boolean(settings.order?.on), blocks: Array.isArray(settings.order?.blocks) ? settings.order.blocks : [] };
  fallback.loaded = true;
}

const errorText = (error) => error?.message ?? String(error ?? "");

export async function loadFallback() {
  try {
    take(await api.fallback());
    fallback.error = "";
  } catch (error) {
    fallback.error = errorText(error);
  }
}

onEvent("config_changed", () => {
  if (fallback.loaded) loadFallback();
});

let saving = Promise.resolve();

// Saves the order; shown at once, the engine's answer replaces it.
export function saveOrder(order) {
  fallback.order = order;
  saving = saving.then(async () => {
    try {
      take(await api.setFallbackOrder(order));
      fallback.error = "";
    } catch (error) {
      fallback.error = errorText(error);
      notify("error", `Couldn't save the fallback order: ${errorText(error)}`);
    }
  });
  return saving;
}

// A terminal tab that runs the sign-in command (the tool's own login
// screen opens in the browser).
async function signInTab(login) {
  if (!login?.login_argv?.length) return;
  const tab = await openTerminal({ argv: login.login_argv, title: `Sign in to ${login.account?.name ?? "the account"}` });
  if (tab?.id) showTerminal(tab.id);
}

export async function addAccount(kind) {
  try {
    const login = await api.addAccount(kind);
    await loadFallback();
    await signInTab(login);
  } catch (error) {
    notify("error", `Couldn't add the account: ${errorText(error)}`);
  }
}

export async function signInAccount(id) {
  try {
    await signInTab(await api.signInAccount(id));
  } catch (error) {
    notify("error", `Couldn't start the sign-in: ${errorText(error)}`);
  }
}

export async function renameAccount(id, name) {
  try {
    take(await api.renameAccount(id, name));
  } catch (error) {
    notify("error", `Couldn't rename the account: ${errorText(error)}`);
  }
}

export async function removeAccount(id) {
  try {
    take(await api.removeAccount(id));
  } catch (error) {
    notify("error", `Couldn't remove the account: ${errorText(error)}`);
  }
}

// Spends one of a Codex account's free refills; the new limits arrive as a
// limits event.
export async function useRefill(kind, account) {
  try {
    const result = await api.useRefill(kind, account ?? "");
    const outcome = result?.outcome ?? "";
    if (outcome === "reset" || outcome === "alreadyRedeemed") notify("info", "Usage refilled.");
    else if (outcome === "nothingToReset") notify("info", "Nothing to refill yet: the usage isn't used up.");
    else if (outcome === "noCredit") notify("info", "No free refills are left.");
  } catch (error) {
    notify("error", `Couldn't use a refill: ${errorText(error)}`);
  }
}
