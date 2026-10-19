// The palette's user and MCP entries (desktop-design-5-6 2.2): read when the
// palette opens and again on mcp_event prompts_list_changed or state_changed
// while it is open (commands.go:398-417, CMD-011).
import { api } from "../engine/api.js";
import { onEvent } from "../engine/events.js";

export const palette = $state({
  commands: [], // CustomCommand[] (G-7)
  prompts: [], // MCPPrompt[]
  generating: false, // an MCP prompt is being fetched (CMD-010, DLG-263)
});

let open = 0;
let promptSeq = 0;

// One MCP prompt fetch at a time: → its ticket; `current(ticket)` is false once
// it was cancelled or another started (DLG-263).
export function startPrompt() {
  palette.generating = true;
  return ++promptSeq;
}

export const promptCurrent = (ticket) => ticket === promptSeq && palette.generating;

export function finishPrompt(ticket) {
  if (ticket === promptSeq) palette.generating = false;
}

// Esc, or the sheet that showed it closing.
export function cancelPrompt() {
  promptSeq += 1;
  palette.generating = false;
}

export async function readCommands() {
  const [commands, prompts] = await Promise.allSettled([api.customCommands(), api.mcpPrompts()]);
  if (commands.status === "fulfilled") palette.commands = Array.isArray(commands.value) ? commands.value : [];
  if (prompts.status === "fulfilled") palette.prompts = Array.isArray(prompts.value) ? prompts.value : [];
}

// → stop()
export function watchCommands() {
  open += 1;
  readCommands();
  let stopped = false;
  return () => {
    if (!stopped) {
      stopped = true;
      open -= 1;
    }
  };
}

onEvent("mcp_event", (_kind, event) => {
  if (open > 0 && (event?.type === "prompts_list_changed" || event?.type === "state_changed")) readCommands();
});
