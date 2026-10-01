// WebView2 at Windows 150% × text size 125% reports fractional rectangles.
// Its actions' right edge can exceed the header's padding boundary by 0.00002
// CSS pixels. After widening the window, the tabs must leave their compact
// layout and return to the centre; genuine overflow must still be noticed.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
function serve(lang) {
  return async (route) => {
    const url = new URL(route.request().url());
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript",
      body: `window.bootPrefs={lang:"${lang}",theme:"light",web:false,textSize:125};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window={};" });
    if (url.pathname === "/api/state") return route.fulfill({ json: { agents: [], profiles: [], settings: { lang, theme: "light", textSize: 125 } } });
    if (url.pathname === "/api/providers") return route.fulfill({ json: { providers: [], presets: [], excluded: [], gateway: { running: true, window: true } } });
    if (url.pathname === "/api/groups") return route.fulfill({ json: { groups: [] } });
    if (url.pathname.startsWith("/api/")) return route.fulfill({ json: {} });
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    return route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

async function settled(page) {
  await page.evaluate(() => new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve))));
}
async function geometry(page) {
  return page.evaluate(() => {
    const n = document.querySelector("#nav").getBoundingClientRect();
    return { classes: document.querySelector(".top").className, centre: n.x + n.width / 2, viewport: innerWidth };
  });
}

for (const lang of ["en", "zh"]) {
  test(`Windows ${lang}: rounded header edge recovers after widening`, async (t) => {
    const browser = await chromium.launch({ channel: "chromium" });
    t.after(() => browser.close());
    const context = await browser.newContext({ viewport: { width: lang === "zh" ? 440 : 560, height: 600 }, deviceScaleFactor: 1.875, reducedMotion: "reduce" });
    await context.addInitScript(() => {
      Object.defineProperty(Navigator.prototype, "platform", { get: () => "Win32" });
      // Reproduce the measured native rectangle rounding at the browser seam,
      // while retaining the real DOM, styles, ResizeObserver and fitTop code.
      window.headerRightDrift = 0.00002;
      const original = Element.prototype.getBoundingClientRect;
      Element.prototype.getBoundingClientRect = function () {
        const r = original.call(this);
        return this.classList.contains("actions")
          ? new DOMRect(r.x, r.y, r.width + window.headerRightDrift, r.height) : r;
      };
    });
    const page = await context.newPage();
    const errors = [];
    page.on("pageerror", (e) => errors.push(e.message));
    await page.route("**/*", serve(lang));
    await page.goto("http://magpie.test/", { waitUntil: "networkidle" });
    await settled(page);
    await page.setViewportSize({ width: 1400, height: 600 });
    await settled(page);
    const wide = await geometry(page);
    assert.equal(wide.classes.includes("cramped"), false, `wide header stayed compact: ${JSON.stringify(wide)}`);
    assert.ok(Math.abs(wide.centre - wide.viewport / 2) < 0.1, `tabs are off centre: ${JSON.stringify(wide)}`);

    await page.evaluate(() => { window.headerRightDrift = 2; });
    await page.setViewportSize({ width: 1300, height: 600 });
    await settled(page);
    assert.equal((await geometry(page)).classes.includes("crowded"), true, "real overflow was ignored");
    await page.evaluate(() => { window.headerRightDrift = 0.00002; });
    await page.setViewportSize({ width: 1400, height: 600 });
    await settled(page);
    assert.equal((await geometry(page)).classes.includes("cramped"), false, "compact layout was kept after overflow ended");
    assert.deepEqual(errors, []);
  });
}
