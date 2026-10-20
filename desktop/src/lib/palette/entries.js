// The palette's System entries (desktop-design-5-6 2.2's table; the
// terminal's order and conditions, internal/ui/dialog/commands.go:461-682).
// Each entry runs an action (lib/actions.js) or opens a sheet that its owner
// registers; an entry whose action or sheet is not registered (yet) is
// hidden, not broken. Labels that depend on state read the same config
// values as the terminal.
import { actions, runAction } from "../actions.js";
import { dialogRegistered, openDialog } from "../dialogs.js";
import { capabilities } from "../models/capabilities.svelte.js";
import { sessions } from "../stores/sessions.svelte.js";
import { panelSetting, ui } from "../stores/ui.svelte.js";
import { ws } from "../stores/workspace.svelte.js";
import { quitNow } from "./actions.js";

const hasAction = (id) => actions.some((action) => action.id === id);
const actionEnabled = (id) => {
  const action = actions.find((a) => a.id === id);
  return Boolean(action) && (!action.enabled || action.enabled());
};
const tui = () => ws.config?.options?.tui ?? {};
const hasSession = () => Boolean(sessions.currentId);
const coder = () => ws.config?.models?.large ?? {};
const agentModel = () => ws.agent?.model ?? null;

// {id, label, shortcut, aliases, shown(), run()}
function action(id, label, actionId, { shortcut = "", aliases = [], when = () => true } = {}) {
  return { id, label, shortcut, aliases, shown: () => hasAction(actionId) && when(), run: () => runAction(actionId) };
}

function sheet(id, label, dialogId, { shortcut = "", aliases = [], when = () => true } = {}) {
  return { id, label, shortcut, aliases, shown: () => dialogRegistered(dialogId) && when(), run: () => openDialog(dialogId) };
}

function own(id, label, run, { shortcut = "", aliases = [], when = () => true } = {}) {
  return { id, label, shortcut, aliases, shown: when, run };
}

// PAL's own entries (2.2 below the table) come from lib/palette/actions.js;
// they are passed in so this list stays a plain table.
export function systemEntries({ summarize, toggleYolo, initProject }) {
  return [
    action("new_session", "New Session", "chat.new", { shortcut: "ctrl+n", aliases: ["clear"] }),
    sheet("switch_session", "Sessions", "sessions", { shortcut: "ctrl+s" }),
    sheet("switch_model", "Switch Model", "models", { shortcut: "ctrl+l" }),
    sheet("usage", "Usage", "usage"),
    sheet("autocompact", "Fallback compaction", "autocompact"),
    action("undo-file-restore", "Undo Last File Restore", "restore.undo", { when: hasSession }),
    action("tree", "Session Tree", "tree.open", { when: hasSession }),
    action("fork", "Fork from Prompt", "tree.fork", { when: hasSession }),
    action("clone", "Clone Current Branch", "tree.clone", { when: hasSession }),
    own("summarize", "Summarize Session", summarize, { aliases: ["compact"], when: hasSession }),
    {
      ...action("toggle_fast_mode", "", "model.fast", { aliases: ["fast", "speed"], when: () => capabilities.supports_fast }),
      get label() {
        return `${coder().service_tier === "fast" ? "Disable" : "Enable"} FAST Mode`;
      },
    },
    {
      ...action("toggle_ultracode", "", "model.ultracode", { aliases: ["ultracode", "ultra", "workflows"], when: () => capabilities.supports_ultracode }),
      get label() {
        return `${coder().ultracode ? "Turn Off" : "Turn On"} Ultracode`;
      },
    },
    {
      ...action("toggle_thinking", "", "model.thinking", { when: () => agentModel()?.can_reason === true && capabilities.supports_thinking_toggle }),
      get label() {
        return `${coder().think ? "Disable" : "Enable"} Thinking Mode`;
      },
    },
    sheet("select_reasoning_effort", "Select Reasoning Effort", "reasoning", {
      aliases: ["effort"],
      when: () => agentModel()?.can_reason === true && (agentModel()?.reasoning_levels?.length ?? 0) > 0,
    }),
    action("toggle_sidebar", "Toggle Sidebar", "sidebar.toggle", { when: () => hasSession() && (!ui.panelHidden || panelSetting.off) }),
    sheet("file_picker", "Open File Picker", "filepicker", { shortcut: "ctrl+f", when: () => hasSession() && agentModel()?.supports_attachments === true }),
    action("open_external_editor", "Open External Editor", "editor.open", { shortcut: "ctrl+o" }),
    action("enable_docker_mcp", "Enable Docker MCP Catalog", "docker.enable", { when: () => actionEnabled("docker.enable") }),
    action("disable_docker_mcp", "Disable Docker MCP Catalog", "docker.disable", { when: () => actionEnabled("docker.disable") }),
    sheet("toggle_mcps", "Toggle MCPs", "mcp_toggles", { when: () => Object.keys(ws.config?.mcp ?? {}).length > 0 }),
    sheet("select_notifications", "Notification Style", "notifications"),
    own("toggle_yolo", "Toggle Yolo Mode", toggleYolo, { shortcut: "ctrl+y" }),
    action("toggle_help", "Toggle Help", "help.toggle", { shortcut: "ctrl+g" }),
    own("init", "Initialize Project", initProject),
    action("save_project", "Save Project", "project.save"),
    sheet("open_project", "Open Project", "projects", { aliases: ["projects"] }),
    sheet("background", "Background Processes", "background", { shortcut: "ctrl+x", aliases: ["processes", "kill"] }),
    sheet("sub_agents", "Sub-agents", "sub-agents", { aliases: ["agents", "tasks", "stop"] }),
    action("remote", "Remote Control", "remote.open", { aliases: ["rc", "phone"] }),
    action("update_clis", "Update Agent CLIs", "updates.clis", { aliases: ["upgrade", "version"] }),
    action("update_crush", "Update Crush", "updates.crush", { aliases: ["upgrade", "upstream", "release"] }),
    sheet("skills", "Skills", "skills", { aliases: ["install skills", "enable skills", "disable skills"] }),
    sheet("kept_skills", "Toggle skills", "kept_skills", { aliases: ["keep skills on", "remind skills every message", "bring skills back after compression"] }),
    {
      ...action("toggle_composer_focus", "", "composer.focusOnly", { aliases: ["tab", "focus"] }),
      get label() {
        return tui().composer_focus_only === true ? "Let Tab Move Focus" : "Keep Focus on Composer";
      },
    },
    {
      ...action("toggle_thought_line", "", "thoughtline.toggle", { aliases: ["thinking", "thoughts", "status", "working"] }),
      get label() {
        return tui().thought_line === false ? "Show Thought Line" : "Hide Thought Line";
      },
    },
    action("customize_composer", "Customize Composer", "composer.customize", { aliases: ["footer", "stats", "status"] }),
    sheet("subagent_instructions", "Sub-agent Instructions", "subagent_instructions", { aliases: ["subagents", "sub-agents", "spawn", "agents"] }),
    sheet("memory_limit", "Memory Limit", "memory_limit", { aliases: ["ram", "memory", "swap"], when: () => platform.linux }),
    sheet("switch_theme", "Themes", "theme"),
    {
      ...action("toggle_agent_cpus", "", "cpus.toggle", { aliases: ["cpu", "cores", "stutter"], when: () => platform.linux }),
      get label() {
        return ws.config?.options?.agent_cpus_enabled === true ? "Let AI Work Use Whole CPU" : "Run AI Work on CPU Half 2";
      },
    },
    sheet("settings", "Settings", "settings", { shortcut: "ctrl+,", aliases: ["preferences", "options"] }),
    sheet("log_viewer", "Logs", "logs", { aliases: ["log"] }),
    action("usage_report", "Usage report", "usage.report", { aliases: ["stats"] }),
    action("refresh_providers", "Refresh provider list", "providers.refresh", { aliases: ["providers", "catalog"] }),
    sheet("about", "About Crush", "about", { aliases: ["version", "folders"] }),
    own("quit", "Quit", quitNow, { shortcut: "ctrl+c", aliases: ["exit"] }),
  ];
}

// app_info's platform, set once by the plugin.
export const platform = { linux: true };

// The text an entry is filtered on: its label and its aliases
// (commands.go's CommandItem.Filter).
export const entryFilter = (entry) => [entry.label, ...entry.aliases].join(" ");
