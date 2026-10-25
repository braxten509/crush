// Choosing a model (desktop-design-5-6 2.3 "Choosing", steps 1-8; a port of
// internal/ui/model/model_select.go:120-296 and config_write.go:53-69).
// A provider without a usable credential goes to AUTH's sign-in sheets first
// (openDialog("auth_method", …), 7.4); when the sign-in is done the same
// choice runs again.
import { windowBusy } from "../busy.js";
import { closeDialog, hasDialog, openDialog } from "../dialogs.js";
import { api, SCOPE } from "../engine/api.js";
import { loadAgent } from "../stores/workspace.svelte.js";
import { notify } from "../stores/ui.svelte.js";
import { modelInfo } from "./providers.js";
import { modelData, refreshModels } from "./store.svelte.js";
import { saveModelChoice } from "./toggles.js";

const BUSY = "Agent is busy, please wait...";
const HYPER_REFRESH_MS = 15000;

const errorText = (error) => error?.message ?? String(error);

function withLimit(promise, ms) {
  let timer;
  return Promise.race([promise, new Promise((_, reject) => (timer = setTimeout(() => reject(new Error("timed out")), ms)))]).finally(() => clearTimeout(timer));
}

// AUTH's sheets, then the same choice again once signed in.
function signIn(choice, reAuthenticate) {
  closeDialog("models");
  openDialog(
    "auth_method",
    {
      providerId: choice.provider.id,
      providerName: choice.provider.name,
      reAuthenticate,
      onboarding: choice.onboarding,
      model: choice.model,
      modelType: choice.modelType,
      onDone: async (result) => {
        if (result !== "done") {
          if (choice.onboarding) openDialog("models", { onboarding: true });
          return;
        }
        await refreshModels();
        chooseModel({ ...choice, reAuthenticate: false, signedIn: true });
      },
    },
    { grace: true },
  );
}

// choice: {provider: {id, name}, model: SelectedModel, modelType: "large" |
// "small", reAuthenticate?, onboarding?, copilotImported?, signedIn?}.
export async function chooseModel(choice) {
  const { provider, model, modelType, reAuthenticate = false, onboarding = false } = choice;
  // Step 1: an open sign-in sheet may still be dismissed (model_select.go:124-126).
  if (windowBusy() && !hasDialog("oauth")) {
    notify("warn", BUSY);
    return false;
  }
  if (!modelData.cfg) await refreshModels();
  const cfg = modelData.cfg;
  if (!cfg) {
    notify("error", "configuration not found");
    return false;
  }
  const providerId = provider.id;
  const configured = cfg.providers.get(providerId);

  // Step 2: an expired Hyper sign-in is refreshed quietly first.
  if (!reAuthenticate && providerId === "hyper" && configured?.has_oauth && configured.oauth_expired) {
    try {
      await withLimit(api.refreshOAuth(SCOPE.global, "hyper"), HYPER_REFRESH_MS);
      await refreshModels();
    } catch {
      signIn(choice, false);
      return false;
    }
    return chooseModel({ ...choice, reAuthenticate: false });
  }

  // Step 3: Copilot's token from VS Code, once.
  if (providerId === "copilot" && !configured && !reAuthenticate && !choice.copilotImported) {
    try {
      await api.importCopilot();
    } catch {
      // No token to import: the sign-in sheet follows.
    }
    await refreshModels();
    return chooseModel({ ...choice, copilotImported: true });
  }

  // Step 4: OpenAI and xAI need a sign-in or a key.
  const oneCredential = providerId === "openai" || providerId === "xai";
  if (oneCredential && !configured?.has_oauth && !configured?.has_api_key) {
    signIn(choice, reAuthenticate);
    return false;
  }

  // Step 5: no credential yet, or Ctrl+E.
  if (!configured || reAuthenticate) {
    signIn(choice, reAuthenticate);
    return false;
  }

  // Step 6: a ChatGPT or Grok account may not offer the model.
  if (oneCredential && configured.has_oauth) {
    const offered = (providerId === "openai" ? configured.chatgpt_models : configured.grok_models) ?? [];
    if (!offered.some((m) => m.id === model.model)) {
      closeDialog("models");
      notify("error", `${model.model} isn't offered by your ${provider.name || providerId} account; choose another model`);
      openDialog("models", onboarding ? { onboarding: true } : {});
      return false;
    }
  }

  // Step 7: save, filling the helper model when none is set. A chat's model
  // is saved with the chat (or as the last pick with none open); the helper
  // model is set in config.
  closeDialog("api_key_input");
  closeDialog("oauth");
  closeDialog("models");
  const helper = modelType === "small";
  const fillSmall = !cfg.models?.small && !helper;
  try {
    if (helper) await api.setHelperModel(model);
    else await saveModelChoice(model);
  } catch (error) {
    notify("error", errorText(error));
    return false;
  }
  if (fillSmall) {
    try {
      const small = await api.defaultSmallModel(providerId);
      if (small) await api.setHelperModel(small);
    } catch (error) {
      notify("error", errorText(error));
    }
  }

  // Step 8: onboarding starts the agent; a new helper model is built into
  // it. A chat's next turn builds the chat's own model.
  const name = modelInfo(cfg, modelData.catalog, model)?.name || model.model;
  try {
    if (onboarding) await api.initAgent();
    if (helper || fillSmall) await api.updateAgent();
    notify("info", `${helper ? "Helper model" : "Model"} changed to ${name}`);
  } catch (error) {
    notify("error", errorText(error));
  }
  loadAgent();
  refreshModels();
  return true;
}
