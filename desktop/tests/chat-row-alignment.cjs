// Requires the local Vite server; run through gpu-screen run --net.
const { app, BrowserWindow } = require("electron");
const assert = require("node:assert/strict");
app.commandLine.appendSwitch("force-device-scale-factor", "2");
app.whenReady().then(async () => {
  try {
    const win = new BrowserWindow({ width: 500, height: 200, frame: false });
    await win.loadURL("http://127.0.0.1:5189/tests/chat-row-alignment.html");
    for (const size of [10.6667, 16]) {
      const measurements = await win.webContents.executeJavaScript(`(async () => {
        document.documentElement.style.fontSize = "${size}px";
        await document.fonts.ready;
        await new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)));
        const middle = el => { const r = el.getBoundingClientRect(); return r.y + r.height / 2; };
        const capMiddle = el => {
          const marker = document.createElement('i');
          marker.style.cssText = 'display:inline-block;width:0;height:0';
          el.append(marker);
          const baseline = marker.getBoundingClientRect().y;
          const bottom = el.getBoundingClientRect().bottom;
          marker.remove();
          const ctx = document.createElement('canvas').getContext('2d');
          ctx.font = getComputedStyle(el).font;
          if (baseline + ctx.measureText('g').actualBoundingBoxDescent > bottom + 0.5) {
            throw new Error('Text descenders must fit below the baseline');
          }
          return baseline - ctx.measureText('H').actualBoundingBoxAscent / 2;
        };
        return [...document.querySelectorAll('.chat')].map(row => ({
          title: capMiddle(row.querySelector('.title')),
          status: capMiddle(row.querySelector('.state > span')),
          icons: [...row.querySelectorAll('svg')].map(middle),
          row: middle(row), scale: devicePixelRatio,
        }));
      })()`);
      assert.equal(measurements.length, 2, "both real chat rows mounted");
      for (const row of measurements) {
        for (const [name, center] of Object.entries({ title: row.title, status: row.status })) {
          assert.ok(Math.abs(center - row.icons[0]) * row.scale <= 1,
            `${size}px ${name} capital center ${center} must match icon center ${row.icons[0]} within one physical pixel`);
        }
        assert.ok(Math.abs(row.icons[0] - row.icons[1]) * row.scale <= 1, "status and close icons align");
      }
    }
    console.log("PASS: real chat titles, status text and icons align at compact and full size (2x)");
    app.exit(0);
  } catch (error) { console.error(error); app.exit(1); }
});
