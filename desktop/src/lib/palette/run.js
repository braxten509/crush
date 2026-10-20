// Running the palette's User and MCP entries (commands.go: custom commands,
// skills and MCP prompts), shared by the Ctrl+P palette and the composer's
// `/` menu (owner, 2026-10-20) so both run an entry the same way.
import { closeDialog, openDialog } from "../dialogs.js";
import { api } from "../engine/api.js";
import { sessions } from "../stores/sessions.svelte.js";
import { attachSkill } from "../stores/composer.svelte.js";
import { notify } from "../stores/ui.svelte.js";
import { sendPrompt } from "./actions.js";
import { commandArguments, fillArguments, promptArguments } from "./customcommands.js";
import { finishPrompt, palette, promptCurrent, startPrompt } from "./store.svelte.js";

export const PALETTE_ID = "commands";

// The User category: custom commands and skills.
export function userEntries() {
  return palette.commands.map((command) => ({
    id: `custom_${command.id}`,
    label: command.skill ? command.skill.name || command.name : command.name,
    detail: command.skill?.description ?? "",
    shortcut: "",
    aliases: [],
    run: () => runCommand(command),
  }));
}

// The MCP category: the servers' prompts.
export function mcpEntries() {
  return palette.prompts.map((prompt) => ({
    id: `mcp_${prompt.id}`,
    label: prompt.prompt_id,
    detail: "",
    shortcut: "",
    aliases: [],
    // Without arguments the palette stays, showing "Generating Prompt...".
    keepOpen: promptArguments(prompt).length === 0,
    run: () => runPrompt(prompt),
  }));
}

export function runCommand(command) {
  if (command.skill) {
    attachSkill({ id: command.skill.skill_file_path, name: command.skill.name || command.name });
    return;
  }
  const args = commandArguments(command);
  if (args.length === 0) {
    sendPrompt(command.content);
    return;
  }
  openDialog("arguments", {
    title: "Custom command arguments",
    args,
    onSubmit: (values) => {
      closeDialog("arguments");
      sendPrompt(fillArguments(command.content, values));
    },
  });
}

async function fetchPrompt(prompt, values) {
  const ticket = startPrompt();
  const sid = sessions.currentId;
  try {
    const answer = await api.getMcpPrompt({ client_id: prompt.client_id, prompt_id: prompt.prompt_id, args: values });
    if (!promptCurrent(ticket) || sessions.currentId !== sid) return; // cancelled, replaced, or another chat
    closeDialog("arguments");
    closeDialog(PALETTE_ID);
    if (answer?.prompt) sendPrompt(answer.prompt);
  } catch (error) {
    if (promptCurrent(ticket)) notify("error", error?.message ?? String(error));
  } finally {
    finishPrompt(ticket);
  }
}

export function runPrompt(prompt) {
  const args = promptArguments(prompt);
  if (args.length === 0) {
    fetchPrompt(prompt, {});
    return;
  }
  openDialog("arguments", {
    title: prompt.title || "MCP Prompt Arguments",
    args,
    onSubmit: (values) => fetchPrompt(prompt, values),
  });
}
