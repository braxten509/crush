// The owner's rule (2026-10-24): anything with "test" in its path stays out
// of the Changes view and the FILES list, whatever the case and wherever it
// sits. One helper so both read the same rule.
export function isTestPath(path) {
  return String(path ?? "")
    .toLowerCase()
    .includes("test");
}

export function withoutTestPaths(files) {
  return (Array.isArray(files) ? files : []).filter((file) => !isTestPath(file?.path));
}
