import { defineConfig } from "vite";
import { configDefaults } from "vitest/config";
import { svelte } from "@sveltejs/vite-plugin-svelte";

// Tauri loads the built files from dist/; nothing is served over the network.
// Unit tests run in Node; the renderer's tests (src/lib/render) and the sheet
// stack's (it mounts OverlayHost) run in a page model (jsdom), with Svelte's
// browser build. Vitest 5 has no environmentMatchGlobs, so each is a project.
const PAGE_TESTS = ["src/lib/render/**/*.test.js", "src/lib/dialogs.test.js"];

export default defineConfig({
  plugins: [svelte()],
  clearScreen: false,
  build: { outDir: "dist", target: "es2022", emptyOutDir: true },
  test: {
    projects: [
      {
        extends: true,
        test: {
          name: "node",
          include: ["src/**/*.test.js"],
          exclude: [...configDefaults.exclude, ...PAGE_TESTS],
          environment: "node",
        },
      },
      {
        extends: true,
        resolve: { conditions: ["browser"] },
        test: { name: "jsdom", include: PAGE_TESTS, environment: "jsdom" },
      },
    ],
  },
});
