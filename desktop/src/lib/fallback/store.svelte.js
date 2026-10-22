// The accounts and the fallback order as the engine holds them (GET
// fallback, internal/backend/fallback.go), read when Settings shows them and
// again on config_changed, so a change made elsewhere shows here too. Every
// change is saved at once; the engine's answer, when there is one, replaces
// what is shown.
import { api } from "../engine/api.js";
import { onEvent } from "../engine/events.js";
import { notify } from "../stores/ui.svelte.js";
import { sign_in_run } from "../engine/commands.js";

export const fallback = $state({
  loaded: false,
  accounts: [], // proto.AccountInfo[]: each tool's own login (id "") first
  order: { on: false, blocks: [], lists: [] }, // lists: each picked model's own blocks
  error: "",
});

function take(settings) {
  if (!settings || typeof settings !== "object") return;
  fallback.accounts = Array.isArray(settings.accounts) ? settings.accounts : [];
  const order = settings.order ?? {};
  fallback.order = {
    on: Boolean(order.on),
    blocks: Array.isArray(order.blocks) ? order.blocks : [],
    lists: (Array.isArray(order.lists) ? order.lists : []).map((l) => ({ ...l, blocks: Array.isArray(l?.blocks) ? l.blocks : [] })),
  };
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

// Saves the order; shown at once, the engine's answer replaces it. A refused
// order goes back to the saved one, with the reason above the list.
export function saveOrder(order) {
  fallback.order = order;
  saving = saving.then(async () => {
    try {
      take(await api.setFallbackOrder(order));
      fallback.error = "";
    } catch (error) {
      notify("error", `Couldn't save the fallback order: ${errorText(error)}`);
      // Show what is really saved, not the refused order.
      await loadFallback();
      fallback.error = errorText(error);
    }
  });
  return saving;
}

// Runs the sign-in command with no terminal: the tool opens the default
// browser, and the list shows the account signed in once it finishes.
async function signIn(login) {
  if (!login?.login_argv?.length) return;
  const name = login.account?.name ?? "the account";
  notify("info", `Finish signing in to ${name} in your browser.`, 0);
  let signedIn = false;
  try {
    signedIn = await sign_in_run(login.login_argv);
  } finally {
    await loadFallback();
  }
  if (signedIn) notify("info", `Signed in to ${name}.`);
  else notify("error", `Signing in to ${name} didn't finish.`);
}

export async function addAccount(kind) {
  try {
    const login = await api.addAccount(kind);
    await loadFallback();
    await signIn(login);
  } catch (error) {
    notify("error", `Couldn't add the account: ${errorText(error)}`);
  }
}

export async function signInAccount(id) {
  try {
    await signIn(await api.signInAccount(id));
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
