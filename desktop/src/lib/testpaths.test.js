import { describe, expect, it } from "vitest";
import { isTestPath, withoutTestPaths } from "./testpaths.js";

// The owner's rule (2026-10-24): anything with "test" in its path stays out
// of the Changes view and FILES, whatever the case and wherever it sits.
describe("test paths", () => {
  it("spots test in any part of the path, any case", () => {
    expect(isTestPath("desktop/src/app.test.js")).toBe(true);
    expect(isTestPath("internal/tests/run.go")).toBe(true);
    expect(isTestPath("src/app_test.go")).toBe(true);
    expect(isTestPath("docs/LATEST.md")).toBe(true);
    expect(isTestPath("src/contest/app.js")).toBe(true);
  });

  // Build output is hidden the same way (owner, 2026-10-25): a rebuild's
  // thousands of generated lines are not the agent's changes.
  it("spots files inside build output folders", () => {
    expect(isTestPath("desktop/dist/assets/chunk-UHW7PFBP.js")).toBe(true);
    expect(isTestPath("/home/me/app/node_modules/x/index.js")).toBe(true);
    expect(isTestPath("src-tauri/target/release/app")).toBe(true);
    expect(isTestPath("android/app/build/out.apk")).toBe(true);
    expect(isTestPath("pkg/__pycache__/m.pyc")).toBe(true);
    expect(isTestPath("src/distance.js")).toBe(false);
    expect(isTestPath("src/build.js")).toBe(false);
  });

  it("keeps ordinary paths", () => {
    expect(isTestPath("src/app.js")).toBe(false);
    expect(isTestPath("internal/agent/agent.go")).toBe(false);
    expect(isTestPath("")).toBe(false);
  });

  it("drops the test paths from a file list", () => {
    const files = [{ path: "main.go" }, { path: "main_test.go" }, { path: "tests/run.py" }];
    expect(withoutTestPaths(files).map((f) => f.path)).toEqual(["main.go"]);
    expect(withoutTestPaths(null)).toEqual([]);
  });
});
