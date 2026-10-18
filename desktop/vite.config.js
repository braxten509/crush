import { defineConfig } from "vite";
import { svelte } from "@sveltejs/vite-plugin-svelte";

// Tauri loads the built files from dist/; nothing is served over the network.
export default defineConfig({
  plugins: [svelte()],
  clearScreen: false,
  build: { outDir: "dist", target: "es2022", emptyOutDir: true },
  test: { include: ["src/**/*.test.js"], environment: "node" },
});
