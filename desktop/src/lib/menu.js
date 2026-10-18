// The timeline's right-click menu entries (desktop-design 1.10, 4.2). Phase 3:
// Copy (order 20), Quote in reply (30), Copy whole message (40); phase 4 adds
// Branch from here (10). The menu (P3.5) asks for the entries each time it
// opens, with the context it was opened on.

const items = new Map(); // id → item

// {id, label, order, enabled(ctx), hint(ctx), run(ctx)}; enabled and hint are
// optional. Registering an id again replaces the earlier item.
export function registerMenuItem(item) {
  items.set(item.id, item);
}

// The entries for one context, in order: {id, label, disabled, hint, run()}.
// The hint is shown only while the entry is disabled (4.2).
export function menuItems(ctx) {
  return [...items.values()]
    .sort((a, b) => a.order - b.order)
    .map((item) => ({
      id: item.id,
      label: item.label,
      disabled: item.enabled ? !item.enabled(ctx) : false,
      hint: item.hint ? item.hint(ctx) : "",
      run: () => item.run(ctx),
    }));
}
