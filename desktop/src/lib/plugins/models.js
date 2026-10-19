// MOD's registrations (desktop-design-5-6 2.3, 7.3; loaded by lib/plugins.js):
// the sheets `models`, `reasoning` and `usage`; the actions `model.fast`,
// `model.ultracode`, `model.thinking` (shown by the palette only while the
// coder model supports them) and `providers.refresh` (MISC-019).
import Effort from "../../components/models/Effort.svelte";
import ModelPicker from "../../components/models/ModelPicker.svelte";
import Usage from "../../components/models/Usage.svelte";
import { registerAction } from "../actions.js";
import { registerDialog } from "../dialogs.js";
import { api } from "../engine/api.js";
import { capabilities } from "../models/capabilities.svelte.js";
import { refreshModels } from "../models/store.svelte.js";
import { toggleFast, toggleThinking, toggleUltracode } from "../models/toggles.js";
import { notify } from "../stores/ui.svelte.js";
import { loadProviders } from "../stores/workspace.svelte.js";

registerDialog("models", ModelPicker);
registerDialog("reasoning", Effort);
registerDialog("usage", Usage);

registerAction({ id: "model.fast", title: "Toggle FAST Mode", enabled: () => capabilities.supports_fast, run: toggleFast });
registerAction({ id: "model.ultracode", title: "Toggle Ultracode", enabled: () => capabilities.supports_ultracode, run: toggleUltracode });
registerAction({ id: "model.thinking", title: "Toggle Thinking Mode", enabled: () => capabilities.supports_thinking_toggle, run: toggleThinking });

// G-9: the same work as `crush update-providers`, one notice per source.
let refreshing = false;
registerAction({
  id: "providers.refresh",
  title: "Refresh provider list",
  async run() {
    if (refreshing) return;
    refreshing = true;
    try {
      const answer = await api.refreshProviders();
      for (const result of answer?.results ?? []) {
        if (result.ok) notify("info", `${result.source} provider updated successfully.`);
        else notify("error", result.error || `${result.source} provider update failed`);
      }
      loadProviders();
      refreshModels();
    } catch (error) {
      notify("error", error?.message ?? String(error));
    } finally {
      refreshing = false;
    }
  },
});
