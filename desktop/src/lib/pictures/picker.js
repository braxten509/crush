// The Add Image picker's rules (desktop-design-5-6 2.10; the terminal's
// internal/ui/dialog/filepicker.go over bubbles' filepicker): folders first,
// then files, each by name; hidden entries left out; only .jpg, .jpeg and
// .png can be chosen, whatever their case (DLG-256); 5 MB at most.

export const MAX_BYTES = 5 * 1024 * 1024;
export const TOO_LARGE = "file too large, max 5MB";
export const EMPTY = "Bummer. No Files Found.";
const ALLOWED = [".jpg", ".jpeg", ".png"];

export function choosable(entry) {
  if (entry.is_dir) return false;
  const name = entry.name.toLowerCase();
  return ALLOWED.some((ext) => name.endsWith(ext));
}

// dir_list's entries → the shown rows.
export function shownEntries(entries) {
  const visible = (entries ?? []).filter((entry) => !entry.name.startsWith("."));
  const byName = (a, b) => (a.name < b.name ? -1 : a.name > b.name ? 1 : 0);
  return [...visible.filter((e) => e.is_dir).sort(byName), ...visible.filter((e) => !e.is_dir).sort(byName)];
}

export function joinPath(dir, name) {
  return dir.endsWith("/") ? `${dir}${name}` : `${dir}/${name}`;
}

export function parentOf(dir) {
  if (dir === "/" || !dir) return "/";
  const trimmed = dir.replace(/\/+$/, "");
  const at = trimmed.lastIndexOf("/");
  return at <= 0 ? "/" : trimmed.slice(0, at);
}

export function baseName(path) {
  return path.replace(/\/+$/, "").split("/").pop() || path;
}
