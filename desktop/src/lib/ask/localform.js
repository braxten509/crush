// A yes/no form in the typing box's place with the slot kind "update", never
// sent to the engine (desktop-design-5-6 2.19, 7.4). Placeholder from
// scaffold S5 with the pinned export; ASK owns it (7.3).

// → {close()}
export function askYesNo({ label, question, description, onAnswer }) {
  return { close() {} };
}
