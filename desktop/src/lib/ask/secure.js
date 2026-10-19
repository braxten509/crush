// Secure entry's value rules (desktop-design-5-6 2.7 "Validation"; the
// engine's own, internal/secureentry/secureentry.go:153-168): one trailing
// newline is stripped first; the engine checks again. Values live only in
// the secure field components and the apiSecret call, never here.

const LIMIT = 64 * 1024;

export function cleanValue(value) {
  return String(value ?? "").replace(/\r?\n$/, "");
}

// The field's error text, or "" for a usable value.
export function valueError(value) {
  const text = cleanValue(value);
  if (text === "") return "enter a value before saving";
  if (new TextEncoder().encode(text).length > LIMIT) return "value exceeds 64 KiB";
  // eslint-disable-next-line no-control-regex
  if (/[\u0000-\u001f\u007f]/.test(text)) return "enter a single-line value without control characters";
  return "";
}

// The status texts (secure.go:261-269, :305-315).
export function statusText(field, entered) {
  if (field?.status === "unavailable") return "This file was already saved. Request this entry again to add it.";
  if (field?.status === "saved") return "Saved";
  return entered ? "Entered · saved on submit" : "Enter secret…";
}
