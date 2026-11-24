// The model menu's view of the engine's provider availability
// (providers/availability: a provider with a folder rule; none has one now).
// The store holds the folder's answer; these two read it.

// unavailableReason is why a provider cannot run here, or "".
export function unavailableReason(providerId, byProvider) {
  return byProvider?.[providerId] ?? "";
}

// shortUnavailable drops the engine sentence's lead, for a menu row:
// "this folder is not a git project."
export function shortUnavailable(reason) {
  return String(reason ?? "").replace(/^.*? is not usable here: /u, "");
}
