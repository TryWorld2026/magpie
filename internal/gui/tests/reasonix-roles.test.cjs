// Real assets, isolated API fixtures; no Reasonix process or user configuration.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const options = [
  { value: "magpie/b/executor", label: "B Executor", ref: "b/executor", group: "B", icon: "deepseek-color" },
  { value: "magpie/b/planner", label: "B Planner", ref: "b/planner", group: "B", icon: "deepseek-color" },
  { value: "magpie/a/pro", label: "A Pro", ref: "a/pro", group: "A", icon: "deepseek-color" },
  { value: "magpie/a/flash", label: "A Flash", ref: "a/flash", group: "A", icon: "deepseek-color" },
  { value: "native/old", label: "Native Old", group: "Reasonix Studio" },
];

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    for (const mode of ["window", "panel"]) {
      test(`${engine} ${lang} ${mode}: Reasonix roles keep labels and model controls`, async (t) => {
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        const page = await browser.newPage({ viewport: { width: mode === "panel" ? 440 : 1000, height: mode === "panel" ? 560 : 700 } });
        page.setDefaultTimeout(5000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        const fields = [
          { key: "model", label: "executor", value: "", options },
          { key: "planner", label: "planner", value: "", options: [{ value: "off", label: "off" }, ...options] },
        ];
        await page.addInitScript(() => localStorage.setItem("magpie.modelFavorites", '["a/pro"]'));
        await page.route("**/*", async (route) => {
          const url = new URL(route.request().url());
          const json = (data) => route.fulfill({ json: data });
          if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
          if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
          if (url.pathname === "/api/state") return json({ agents: [{ id: "reasonix", name: "Reasonix Studio", icon: "reasonix-color", path: "/fixture/config.toml", wired: fields.some((f) => f.value.startsWith("magpie/")), fields }], profiles: [], settings: { lang, theme: "light" } });
          if (url.pathname === "/api/providers") return json({ providers: [], gateway: { running: true, window: true } });
          if (url.pathname === "/api/groups") return json({ groups: [] });
          if (url.pathname === "/api/usage/quotas") return json([]);
          if (url.pathname === "/api/agents/cli") return json({ agents: {}, pending: false });
          if (url.pathname.startsWith("/api/")) return json({});
          const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
          const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
          return route.fulfill({ body: await fs.readFile(file), contentType });
        });
        t.after(async () => {
          if (process.env.ARTIFACT_DIR) {
            await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
            await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-${mode}-reasonix-roles.png`) });
          }
          await browser.close();
        });
        const row = page.locator('.row.agent[data-id="reasonix"]');
        const open = async () => {
          await row.waitFor();
          if (mode === "panel") await row.locator(".ag-sum").click();
        };
        await page.goto("http://magpie.test/" + (mode === "panel" ? "?mode=panel" : ""));
        await open();
        if (mode === "window") {
          assert.equal((await row.locator(".ag-start").textContent()).trim(), lang === "zh" ? "选模型" : "Pick a model");
        } else for (const f of fields) {
          assert.equal(await row.locator(`.field[data-key="${f.key}"] > .k`).count(), 1, "a default role must be labelled once");
        }
        fields[0].value = "magpie/b/executor";
        fields[1].value = "magpie/b/planner";
        // Either role keeps the connection while the other is on its default.
        for (const f of fields) {
          const selected = f.value;
          f.value = "";
          await page.reload();
          await open();
          assert.equal(await row.locator(`.field[data-key="${f.key}"] > .k`).count(), 1, "a connected default role must be labelled once");
          f.value = selected;
        }
        await page.reload();
        await open();
        for (const f of fields) {
          const field = row.locator(`.field[data-key="${f.key}"]`);
          assert.equal(await field.locator(":scope > .k").count(), 1, "a selected role must be labelled once");
          await field.click();
          const pop = page.locator("#pop:not([hidden])");
          assert(await pop.evaluate((e) => e.classList.contains("model-picker")), "role lost its model picker");
          assert(await page.locator("#pickerRail").isVisible());
          await page.locator('#pickerRail [data-group="favorites"]').click();
          await page.waitForFunction(() => document.querySelectorAll("#list li[data-i]").length === 1);
          assert.equal(await page.locator("#list li[data-i]").count(), 1);
          assert.match(await page.locator("#list").innerText(), /A Pro/);
          await page.locator('#pickerRail [data-group="A"]').click();
          await page.waitForFunction(() => {
            const text = document.querySelector("#list").innerText;
            return text.includes("A Pro") && text.includes("A Flash") && !text.includes("Native Old");
          });
          const listed = await page.locator("#list").innerText();
          assert.match(listed, /A Pro/);
          assert.match(listed, /A Flash/);
          assert(!listed.includes("Native Old"));
          await page.locator('#pickerRail [data-group="all"]').click();
          await page.locator("#q").fill("magpie/a/typed");
          assert.equal(await page.locator("#list li.custom").count(), 1, "typed model choice is missing");
          await page.keyboard.press("Escape");
        }
        assert.deepEqual(errors, []);
      });
    }
  }
}
