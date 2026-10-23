// Run on the hidden GPU screen: gpu-screen run --net -- node tests/driver/ui_regressions.mjs
import assert from "node:assert/strict";
import { createRequire } from "node:module";
import { mkdir, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { createServer } from "vite";

const root = fileURLToPath(new URL("../../", import.meta.url));
const out = path.join(root, "tests/out/ui_regressions");
const require = createRequire(import.meta.url);
const { chromium } = require(require.resolve("playwright-core", { paths: [path.join(process.env.HOME, ".agents/skills/align/scripts")] }));
await mkdir(out, { recursive: true });
await writeFile(path.join(out, "Fixture.svelte"), `<script>
  import ToolGroup from "/src/components/tools/ToolGroup.svelte";
  import WordMenu from "/src/components/settings/WordMenu.svelte";
  import ChatRow from "/src/components/list/ChatRow.svelte";
  import TerminalRows from "/src/components/leftlist/TerminalRows.svelte";
  import { terminals } from "/src/lib/terminal/tabs.svelte.js";
  let open = $state(false);
  terminals.push({ id: "one", title: "Terminal one", cwd: "/example" }, { id: "two", title: "Terminal two", cwd: "/example" });
  const row = { id: "first", title: "Example chat", state: "", time: "1:23 PM", depth: 0, branches: 0 };
  const item = { key: "overflow", groupId: "overflow", working: true,
    live: { key: "status:group:overflow", words: "Checking a very long command and its output ".repeat(10), startedAt: Date.now(), thought: "" },
    calls: [{ id: "edited", name: "edit", input: JSON.stringify({ file_path: "/example/long-file.js" }),
      result: { content: "done", review: { summary: { files: 1, counted: true, adds: 3, dels: 1, paths: ["/example/long-file.js"] } } } }]
  };
</script>
<main style="padding: 32px; width: 740px">
  <h2>Tool text beside Changes</h2>
  <div id="tools">{#each [240, 360, 640] as width}<div style:width={width + "px"} style="margin: 16px 0"><ToolGroup item={{ ...item, key: 'overflow-' + width, groupId: 'overflow-' + width }} /></div>{/each}</div>
  <h2>Fallback word</h2>
  <WordMenu text="DeepSeek V4.1 Flash" label="Model" {open} ontoggle={(value) => open = value}>{#snippet children(done)}<button data-item onclick={done}>Another model</button>{/snippet}</WordMenu>
  <h2>Chat and terminal rows</h2>
  <ul id="chats"><ChatRow {row} /><ChatRow row={{ ...row, id: 'second', selected: true }} /></ul>
  <TerminalRows />
</main>`);
await writeFile(path.join(out, "index.html"), `<html><head><meta charset="utf-8"></head><body><script type="module">
import '/src/app.css';
import { mount } from 'svelte';
import Fixture from './Fixture.svelte';
mount(Fixture, { target: document.body });
</script></body></html>`);
const server = await createServer({ root, server: { host: "127.0.0.1", port: 0, hmr: false }, clearScreen: false });
let browser;
try {
  await server.listen();
  browser = await chromium.launch({ headless: false, args: ["--ozone-platform=wayland"] });
  const page = await browser.newPage({ viewport: { width: 1000, height: 1050 } });
  const errors = [];
  page.on("pageerror", (error) => errors.push(String(error)));
  await page.goto(`http://127.0.0.1:${server.httpServer.address().port}/tests/out/ui_regressions/index.html`);
  await page.locator("#tools .changes").first().waitFor();
  await page.evaluate(() => document.fonts.ready);
  await page.screenshot({ path: path.join(out, "before.png") });
  const overflow = await page.locator("#tools .group").evaluateAll((groups) => groups.map((group) => {
    const line = group.querySelector(".line");
    const changes = group.querySelector(".changes");
    return {
      ellipses: [line, ...line.querySelectorAll("*")].filter((node) => getComputedStyle(node).textOverflow === "ellipsis").length,
      clipped: line.scrollWidth > line.clientWidth,
      gap: changes.getBoundingClientRect().left - line.getBoundingClientRect().right,
    };
  }));
  assert.equal(errors.length, 0, errors.join("\n"));
  for (const state of overflow) {
    assert.equal(state.clipped, true, "long tool text must exercise truncation");
    assert.equal(state.ellipses, 1, "only the outer tool line may draw an ellipsis");
    assert.ok(state.gap >= 0, "tool text must stop before Changes");
  }
  const word = page.getByRole("button", { name: "Model: DeepSeek V4.1 Flash", exact: true });
  assert.equal(await word.evaluate((el) => getComputedStyle(el).textDecorationStyle), "dotted");
  await word.click();
  assert.equal(await word.evaluate((el) => getComputedStyle(el).textDecorationStyle), "dotted", "opening the menu must keep the dotted underline");
  await page.keyboard.press("Escape");
  for (const selector of ["#chats .chat + .chat", ".terminal + .terminal"]) {
    assert.equal(await page.locator(selector).evaluate((el) => getComputedStyle(el).borderTopWidth), "0px", "rows have no dividing line");
  }
  assert.equal(await page.locator("#chats .selected .row").evaluate((el) => getComputedStyle(el, "::before").borderBottomWidth), "1px", "the selected band keeps its outline");
  await page.screenshot({ path: path.join(out, "checked.png") });
  console.log("PASS: one ellipsis at three widths, room before Changes, dotted open menu, no row dividers, selected outline preserved");
} finally {
  await browser?.close();
  await server.close();
}
