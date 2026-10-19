// The left list's phase 6 row states (desktop-design-5-6 2.20, 6.4; scaffold
// S5, complete): LST registers unread, ready and unknown. ChatRow draws
// rowState(id) on a row that has none of phase 3's own states (waiting,
// working, background). fn(sessionId) → {icon, text} or null; the first
// non-null answer wins, in registration order.
const states = [];

export function registerRowState(fn) {
  states.push(fn);
  return () => {
    const at = states.indexOf(fn);
    if (at >= 0) states.splice(at, 1);
  };
}

export function rowState(sessionId) {
  for (const fn of states) {
    const state = fn(sessionId);
    if (state) return state;
  }
  return null;
}
