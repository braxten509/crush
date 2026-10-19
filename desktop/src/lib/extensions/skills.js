// Skills manager and Toggle skills rules (desktop-design-5-6 2.8;
// internal/ui/dialog/skillbrowser and kept_skills.go).

export const TEXT = {
  loading: "Loading installed skills…",
  searching: "Searching skills.sh…",
  saving: "Saving skill setting…",
  installing: "Installing skill…",
  applied: "Settings apply to subsequent turns in Crush.",
  installed: "Installed in the shared skills folder.",
  already: "Already installed. Switch to Installed to enable or disable it.",
  short: "Type at least two characters to search skills.sh.",
  unavailable: "Skill management is unavailable in this workspace.",
  emptyInstalled: "No installed skills match this filter.",
  emptyBrowse: "Search skills.sh above, then select a skill to install.",
  noResults: "No results. Try another search.",
};

// "system:", "user:", "project:" (DLG-215).
export const labelOf = (skill) => (skill?.label ? `${String(skill.label).replace(/:$/, "")}:` : "");

// Kept skills groups (kept_skills.go:38): not kept → Off, by_agent → Toggleable, else On.
export function keptState(skill, kept) {
  const entry = (kept ?? []).find((k) => k.id === skill.id || k.name === skill.name);
  if (!entry) return "off";
  return entry.by_agent ? "toggleable" : "on";
}

export const GROUP = { on: "On", toggleable: "Toggleable", off: "Off" };
export const GROUP_ORDER = ["on", "toggleable", "off"];

// Enter cycles Off → Toggleable → On → Off (kept_skills.go:207-222) → {on, toggleable}.
export function nextKept(state) {
  if (state === "off") return { on: true, toggleable: true };
  if (state === "toggleable") return { on: true, toggleable: false };
  return { on: false, toggleable: false };
}
