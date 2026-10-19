// Settings → Accounts' sign-in and sign-out (desktop-design-5-6 2.4
// "Settings rows", 7.4; MISC-012, MISC-013). `platform` is a flow platform
// ("hyper", "copilot", "openai", "grok") or its provider id ("xai").
import { hasDialog, openDialog } from "../dialogs.js";
import { api, SCOPE } from "../engine/api.js";
import { refreshModels } from "../models/store.svelte.js";
import { notify } from "../stores/ui.svelte.js";
import { PLATFORMS, providerOfPlatform } from "./flows.js";

const providerOf = (platform) => (PLATFORMS[platform] ? platform : providerOfPlatform(platform));

// The keys `crush logout` removes (internal/cmd/logout.go:131-183).
const LOGOUT_KEYS = {
  hyper: ["providers.hyper.api_key", "providers.hyper.oauth"],
  copilot: ["providers.copilot.api_key", "providers.copilot.oauth"],
  openai: ["providers.openai.oauth", "providers.openai.chatgpt_models", "providers.openai.api_key"],
  xai: ["providers.xai.oauth", "providers.xai.grok_models", "providers.xai.api_key"],
};

// The sign-in sheet without a model choice; "Signed in to <name>" when done.
// → Promise<"done" | "cancelled">
export function signIn(platform) {
  const providerId = providerOf(platform);
  if (!providerId) return Promise.resolve("cancelled");
  const name = PLATFORMS[providerId].name;
  return new Promise((resolve) => {
    if (hasDialog("oauth")) {
      resolve("cancelled");
      return;
    }
    openDialog(
      "oauth",
      {
        providerId,
        providerName: name,
        onDone: (result) => {
          if (result === "done") {
            notify("info", `Signed in to ${name}`);
            refreshModels();
          }
          resolve(result);
        },
      },
      { grace: true },
    );
  });
}

// Asks first, No selected; then removes the keys. → Promise<"done" | "cancelled">
export function signOut(platform) {
  const providerId = providerOf(platform);
  if (!providerId) return Promise.resolve("cancelled");
  const name = PLATFORMS[providerId].logout;
  return new Promise((resolve) => {
    openDialog("signout_confirm", {
      name,
      onAnswer: async (yes) => {
        if (!yes) {
          resolve("cancelled");
          return;
        }
        try {
          for (const key of LOGOUT_KEYS[providerId]) await api.removeConfig(SCOPE.global, key);
          notify("info", `Successfully logged out of ${name}.`);
          refreshModels();
          resolve("done");
        } catch (error) {
          notify("error", error?.message ?? String(error));
          resolve("cancelled");
        }
      },
    });
  });
}
