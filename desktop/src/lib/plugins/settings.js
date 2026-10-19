// SET's registrations (desktop-design-5-6 2.12, 2.13; loaded by
// lib/plugins.js): the dialogs `settings`, `autocompact`, `memory_limit`,
// `subagent_instructions` and `notifications`; the actions `sidebar.toggle`,
// `composer.focusOnly`, `thoughtline.toggle` and `cpus.toggle` (the
// palette's entries and the Settings switches).
import AutoCompact from "../../components/settings/AutoCompact.svelte";
import MemoryLimit from "../../components/settings/MemoryLimit.svelte";
import NotificationStyle from "../../components/settings/NotificationStyle.svelte";
import Settings from "../../components/settings/Settings.svelte";
import SubagentInstructions from "../../components/settings/SubagentInstructions.svelte";
import { registerAction } from "../actions.js";
import { registerDialog } from "../dialogs.js";
import { toggleAgentCpus, toggleComposerFocusOnly, toggleSidebar, toggleThoughtLine } from "../settings/store.svelte.js";

registerDialog("settings", Settings);
registerDialog("autocompact", AutoCompact);
registerDialog("memory_limit", MemoryLimit);
registerDialog("subagent_instructions", SubagentInstructions);
registerDialog("notifications", NotificationStyle);

registerAction({ id: "sidebar.toggle", title: "Toggle Sidebar", run: toggleSidebar });
registerAction({ id: "composer.focusOnly", title: "Keep Focus on Composer", run: toggleComposerFocusOnly });
registerAction({ id: "thoughtline.toggle", title: "Hide Thought Line", run: toggleThoughtLine });
registerAction({ id: "cpus.toggle", title: "Run AI Work on CPU Half 2", run: toggleAgentCpus });
