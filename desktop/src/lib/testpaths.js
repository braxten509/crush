// The owner's rule (2026-10-24): anything with "test" in its path stays out
// of the Changes view and the FILES list, whatever the case and wherever it
// sits. Build output goes too (owner, 2026-10-25): files inside a dist,
// build, target, node_modules or __pycache__ folder are a rebuild's
// generated lines, not the agent's changes. One helper so both read the
// same rule.
const BUILD_FOLDERS = new Set(["dist", "build", "target", "node_modules", "__pycache__"]);

export function isTestPath(path) {
  const text = String(path ?? "");
  if (text.toLowerCase().includes("test")) return true;
  const folders = text.split(/[\\/]/u).slice(0, -1);
  return folders.some((folder) => BUILD_FOLDERS.has(folder));
}

export function withoutTestPaths(files) {
  return (Array.isArray(files) ? files : []).filter((file) => !isTestPath(file?.path));
}
