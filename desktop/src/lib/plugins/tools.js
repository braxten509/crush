// Piece P3.6's registrations (desktop-design 1.10): the Changes sheet, a
// standalone tool's copy text for selections, the timeline keys that act on a selected tool group, tool or shell item, and
// the probe's "tools" section.
//
// Keys (1.8, timeline scope), for the selected item when it is one of ours
// (lib/tools/open.svelte.js registerItem):
//   c y C Y               copy it (CHAT-044, CHAT-056, CHAT-100)
//   Space                 open or close it (CHAT-042, CHAT-097); a focused
//                         button inside it (a tool's line, "… N earlier",
//                         "Changes") gets the key instead
//   Shift+← H, Shift+→ L  side-scroll a shell's output or a group's diffs
//                         (CHAT-043)
import ChangesSheet from "../../components/sheets/ChangesSheet.svelte";
import { registerItemCopy } from "../copy.js";
import { registerDialog } from "../dialogs.js";
import { bindKey } from "../keys.js";
import { probeSection } from "../probe.js";
import { prompts } from "../stores/prompts.svelte.js";
import { ui } from "../stores/ui.svelte.js";
import { TOOL_COPIED, toolCopyText } from "../tools/copy.js";
import { itemActions } from "../tools/open.svelte.js";
import { probeRead } from "../tools/probe.js";
import { toolStatus } from "../tools/status.js";

// The Changes sheet draws its own scrim (outside clicks close it, DLG-308).
registerDialog("changes", ChangesSheet, { veil: false });

// A standalone tool (Question, Agent, Agentic Fetch) inside a timeline
// selection copies as its c/y text (CHAT-056, CHAT-087; lib/copy.js wholeText).
registerItemCopy("tool", (item) => ({ text: toolCopyText(item.call, toolStatus(item.call, prompts.callStatus)), notice: TOOL_COPIED }));

function selected() {
  return ui.selectedItem ? itemActions(ui.selectedItem) : null;
}

for (const combo of ["c", "y", "C", "Y"]) {
  bindKey("timeline", combo, () => {
    const actions = selected();
    if (!actions?.copy) return false;
    actions.copy();
    return true;
  });
}

bindKey("timeline", "Space", () => {
  const focused = document.activeElement;
  if (focused?.closest?.(".group button, .tool-box button")) return false;
  const actions = selected();
  if (!actions?.toggle) return false;
  actions.toggle();
  return true;
});

for (const [combo, dir] of [
  ["Shift+ArrowLeft", -1],
  ["H", -1],
  ["Shift+ArrowRight", 1],
  ["L", 1],
]) {
  bindKey("timeline", combo, () => {
    const actions = selected();
    if (!actions?.scroll) return false;
    return actions.scroll(dir) === true;
  });
}

// Not one of 1.9's pinned sections: P3.6's own, for p3_tools.py.
probeSection(
  "tools",
  () => ({
    groups: probeRead("groups"),
    tools: probeRead("tools"),
    shells: probeRead("shells"),
    diffs: probeRead("diffs"),
    sheet: probeRead("sheet")[0] ?? null,
    selected: ui.selectedItem,
  }),
  () => ({
    groups: probeRead("groups", "heavy"),
    tools: probeRead("tools", "heavy"),
    shells: probeRead("shells", "heavy"),
    diffs: probeRead("diffs", "heavy"),
    sheet: probeRead("sheet", "heavy")[0] ?? null,
    animations: document.getAnimations().length,
  }),
);
