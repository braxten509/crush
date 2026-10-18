// Named actions (desktop-design 1.10): what the window can do by name, for keys
// and buttons now and phase 5's command palette later, which lists `actions`
// and calls runAction. Plugins register them when imported (lib/plugins.js).
import { SvelteMap } from "svelte/reactivity";

const registry = new SvelteMap(); // id → action, in registration order

// Every registered action, as a read-only array Svelte templates track.
export const actions = liveList(() => [...registry.values()]);

// {id, title, keys?, enabled(), run()}. The object is kept as given, so a
// title that depends on state can be a getter ("Toggle To-Dos" or "Toggle
// Queue"). Without enabled() the action is always enabled. Registering an id
// again replaces the earlier action.
export function registerAction(action) {
  registry.set(action.id, action);
}

// Runs the action: false for an unknown or disabled id, else true.
export function runAction(id) {
  const action = registry.get(id);
  if (!action || (action.enabled && !action.enabled())) return false;
  Promise.resolve(action.run()).catch((error) => console.error(`action ${id} failed`, error));
  return true;
}

function liveList(read) {
  return new Proxy([], {
    get(_, key) {
      const list = read();
      const value = Reflect.get(list, key);
      return typeof value === "function" ? value.bind(list) : value;
    },
    has: (_, key) => Reflect.has(read(), key),
    ownKeys: () => Reflect.ownKeys(read()),
    getOwnPropertyDescriptor: (_, key) => Reflect.getOwnPropertyDescriptor(read(), key),
    set: () => false,
    deleteProperty: () => false,
    defineProperty: () => false,
  });
}
