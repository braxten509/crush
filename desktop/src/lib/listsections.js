// Sections of the left list beside the chat rows (desktop-design-5-6 2.20,
// 6.4; scaffold S5, complete): LST's terminal rows ("rows", after the chats)
// and server rows ("footer", above the list's end rule). ChatList draws the
// visible sections of each place by order; "header" sits under the search's
// rule. visible() may read any store.
const sections = new Map(); // id → section

export function registerListSection({ id, order, place, visible = () => true, component }) {
  if (!["header", "rows", "footer"].includes(place)) throw new Error(`registerListSection: unknown place "${place}"`);
  sections.set(id, { id, order, place, visible, component });
  return () => sections.delete(id);
}

// The visible sections of one place, by order.
export function listSections(place) {
  return [...sections.values()].filter((section) => section.place === place && section.visible()).sort((a, b) => a.order - b.order);
}
