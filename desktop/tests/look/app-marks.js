// Alignment marks for every screen of the real app (phase 7 look pass), run
// in the window by tests/driver/look_align.py through the align skill's
// mark() helper. soft() marks only what the screen shows. Text that comes
// from data (chat names, agents, times, states, model names, numbers, list
// items) is "variable": the checker leaves its letters out and measures the
// icons and fixed labels around it.
const slug = (s) => s.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "").slice(0, 32) || "x";
const soft = (selector, options) => {
  if (document.querySelector(selector)) mark(selector, options);
};
const numbered = (prefix) => {
  const seen = new Map();
  return (el) => {
    const base = prefix + "-" + slug(el.textContent);
    const n = (seen.get(base) ?? 0) + 1;
    seen.set(base, n);
    return n === 1 ? base : base + "-" + n;
  };
};
const front = [...document.querySelectorAll("[data-dialog]")].filter((el) => !el.inert).pop();

if (!front) {
  // Left column: the two controls with an icon and fixed text, and the day labels.
  soft(".new-chat", { id: "new-chat", icon: "svg", text: ".label", source: "src/components/ChatList.svelte" });
  soft(".search", { id: "search", icon: "svg", text: "kbd", source: "src/components/ChatList.svelte" });
  soft(".day", { id: numbered("day"), text: ":scope", center: false, source: "src/components/ChatList.svelte" });
  // Chat rows: the open chat's chevron sits on its row's middle; states sit beside text of any length.
  soft(".chat.expanded > .row", { id: numbered("chat-open"), icon: ".chevron", source: "src/components/list/ChatRow.svelte" });
  soft(".row .state:has(svg)", { id: numbered("state"), icon: "svg", text: "span", textKind: "variable", center: false, source: "src/components/list/ChatRow.svelte" });
  // Middle column: tool rows, the model picker's caret and the send arrow.
  soft(".tool-row:has(svg)", { id: numbered("tool-row"), icon: "svg", source: "src/components/tools/ToolEntry.svelte" });
  soft(".model:has(svg)", { id: "model-picker", icon: "svg", text: ".label", textKind: "variable", source: "src/components/composer/ModelButton.svelte" });
  soft(".send:has(svg)", { id: "send", icon: "svg", source: "src/components/composer/Composer.svelte" });
  // Right column: the section labels share one left edge; meter labels; values with a warning icon.
  soft(".panel > section > h2.label", { id: numbered("label"), text: ":scope", column: "panel-label", center: false, source: "src/components/SidePanel.svelte" });
  soft(".usage .label", { id: numbered("meter"), text: ":scope", center: false, source: "src/components/panel/Meters.svelte" });
  soft(".value:has(svg)", { id: numbered("value"), icon: "svg", text: "span", textKind: "variable", center: false, source: "src/components/panel/Meters.svelte" });
} else {
  const S = `[data-dialog="${front.dataset.dialog}"]`;
  // The title and the section labels share the sheet's left edge.
  soft(`${S} [data-kit-title] > .title`, { id: "title", text: ":scope", column: "sheet-left", center: false, source: "src/components/kit/Sheet.svelte" });
  soft(`${S} .kit-section-label`, { id: numbered("section"), text: ":scope", column: "sheet-left", center: false, source: "src/components/kit/Sheet.svelte" });
  // Buttons: the fixed label sits in the middle of its button.
  soft(`${S} [data-kit-buttons] .button`, { id: numbered("button"), text: ".label", source: "src/components/kit/Buttons.svelte" });
  // List rows with an icon: the icon on the row's middle, beside text from data.
  soft(`${S} [data-kit-list] .row:has(svg)`, { id: numbered("row"), icon: "svg", text: ".title", textKind: "variable", source: "src/components/kit/FilterList.svelte" });
  // List group labels share the list's left edge.
  soft(`${S} [data-kit-list] [data-group]`, { id: numbered("group"), text: ":scope", column: "list-left", center: false, source: "src/components/kit/FilterList.svelte" });
  // Help line: each key name with a letter or digit in it ("↑/↓" has no letters to measure).
  for (const pair of document.querySelectorAll(`${S} [data-kit-help] .pair[data-shown]`)) {
    if (/[A-Za-z0-9]/.test(pair.querySelector("kbd")?.textContent ?? "")) pair.dataset.alignHelp = "";
  }
  soft(`${S} [data-align-help]`, { id: numbered("help"), text: "kbd", center: false, source: "src/components/kit/HelpFooter.svelte" });
  // Field icons sit on the field's middle.
  soft(`${S} [data-kit-field].has-icon`, { id: numbered("field"), icon: ".icon", source: "src/components/kit/Field.svelte" });
}
