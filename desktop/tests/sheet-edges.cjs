// Run on the hidden GPU screen: gpu-screen run -- desktop/node_modules/.bin/electron desktop/tests/sheet-edges.cjs
const { app, BrowserWindow } = require("electron");
const { readFileSync } = require("node:fs");
const assert = require("node:assert/strict");
const path = require("node:path");

function rules(file, selector) {
  const source = readFileSync(path.join(__dirname, "../src/components/kit", file), "utf8")
    .split("<style>")[1].replace(/\/\*[\s\S]*?\*\//g, "");
  return [...source.matchAll(/([^{}]+)\{([^{}]*)\}/g)]
    .filter((match) => match[1].trim().split(/,\s*/).includes(selector))
    .map((match) => `${match[1]} {${match[2]}}`).join("\n");
}

app.whenReady().then(async () => {
  const win = new BrowserWindow({ width: 240, height: 240, frame: false });
  try {
    const css = [".edge", ".edge.left", ".edge.right", ".edge.top", ".edge.bottom"]
      .map((selector) => rules("Sheet.svelte", selector)).join("\n") +
      [".fade", ".fade.on", ".fade.top", ".fade.bottom"]
        .map((selector) => rules("FilterList.svelte", selector)).join("\n");
    // Same paint order as Sheet: frame lines, then list contents and fades.
    await win.loadURL(`data:text/html,${encodeURIComponent(`<style>
      :root { --px: 1px; --paper: #222222; --rule-frame: #aaaaaa; }
      body { margin: 0; background: var(--paper); }
      .frame { position: relative; width: 200px; height: 200px; }
      .list { position: relative; width: 100%; height: 100%; }
      .text { position: absolute; left: 99px; width: 2px; height: 100%; background: white; }
      ${css}
      </style><div class="frame">
      <span class="edge left"></span><span class="edge right"></span>
      <span class="edge top"></span><span class="edge bottom"></span>
      <div class="list"><span class="text"></span>
      <span class="fade top on"></span><span class="fade bottom on"></span></div></div>`)}`);
    await win.webContents.executeJavaScript("new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)))");
    const shot = await win.webContents.capturePage();
    const pixels = shot.toBitmap();
    const pixel = (x, y) => pixels[(y * shot.getSize().width + x) * 4];
    for (const x of [0, 199]) {
      for (let y = 0; y < 200; y++) {
        assert.equal(pixel(x, y), 170, `border at ${x},${y} must stay solid under the text fade`);
      }
    }
    assert.ok(pixel(99, 2) < pixel(99, 16), "top text still fades");
    assert.ok(pixel(99, 197) < pixel(99, 183), "bottom text still fades");
    console.log("PASS: solid side borders across both text fades");
    app.exit(0);
  } catch (error) {
    console.error(error);
    app.exit(1);
  }
});
