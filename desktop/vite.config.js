import { defineConfig } from "vite";
import { configDefaults } from "vitest/config";
import { svelte } from "@sveltejs/vite-plugin-svelte";
import { fileURLToPath } from "node:url";

// Electron loads the built files from dist/ (electron/main.cjs serves them as
// app://localhost); nothing is served over the network. The app's imports of
// Tauri's page functions go to src/lib/bridge/, which runs them on Electron.
// Unit tests run in Node; the renderer's tests (src/lib/render) and the sheet
// stack's (it mounts OverlayHost) run in a page model (jsdom), with Svelte's
// browser build. Vitest 5 has no environmentMatchGlobs, so each is a project.
const PAGE_TESTS = ["src/lib/render/**/*.test.js", "src/lib/dialogs.test.js"];

const bridge = (file) => fileURLToPath(new URL(`./src/lib/bridge/${file}`, import.meta.url));

export default defineConfig({
  plugins: [svelte()],
  resolve: {
    alias: {
      "@tauri-apps/api/core": bridge("core.js"),
      "@tauri-apps/api/webviewWindow": bridge("window.js"),
      "@tauri-apps/api/window": bridge("window.js"),
    },
  },
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
