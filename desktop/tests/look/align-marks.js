// Alignment marks for the first screen (src/components), read by
// tests/look/align_real.py through the align skill's mark() helper. Text that
// comes from data (chat names, agents, times, states, model names, numbers) is
// "variable": the checker leaves its letters out and measures the icons and
// fixed labels around it.
const slug = (s) => s.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "").slice(0, 32);

// Left column: the two controls with an icon and fixed text, and the day label.
mark(".new-chat", { id: "new-chat", icon: "svg", text: ".label", source: "src/components/ChatList.svelte:14" });
mark(".search", { id: "search", icon: "svg", text: "kbd", source: "src/components/ChatList.svelte:19" });
mark(".day", { id: (el) => "day-" + slug(el.textContent), text: ":scope", center: false, source: "src/components/ChatList.svelte:29" });

// Chat rows: the open chat's chevron sits on its row's middle.
mark(".chat.expanded > .row", { id: "chat-open", icon: ".chevron", source: "src/components/ChatList.svelte:40" });
// States are right-aligned beside text of any length, so their icons share no column.
mark(".row .state", { id: (el) => "state-" + slug(el.textContent), icon: "svg", text: "span", textKind: "variable", center: false, source: "src/components/ChatList.svelte:43" });

// Middle column: the tool row's triangle, the model picker's caret and the send arrow.
mark(".tool-row", { id: "tool-row", icon: "svg", source: "src/components/Conversation.svelte:42" });
mark(".model", { id: "model-picker", icon: "svg", source: "src/components/Conversation.svelte:59" });
mark(".send", { id: "send", icon: "svg", source: "src/components/Conversation.svelte:64" });

// Right column: the section labels share one left edge; the warning sits beside its number.
mark(".panel > section > h2.label", { id: (el) => "label-" + slug(el.textContent), text: ":scope", column: "panel-label", center: false, source: "src/components/SidePanel.svelte:26" });
mark(".usage .label", { id: (el) => "meter-" + slug(el.textContent), text: ":scope", center: false, source: "src/components/SidePanel.svelte:36" });
mark(".value:has(svg)", { id: (el) => "value-" + slug(el.textContent), icon: "svg", text: "span", textKind: "variable", center: false, source: "src/components/SidePanel.svelte:37" });
