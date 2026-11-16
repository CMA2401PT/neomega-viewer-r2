// 独立验证默认场景，使用真实流式加载、核心解码和 Chrome Metal 截图。
const { chromium } = require(
  process.env.PLAYWRIGHT_MODULE ||
    "/Applications/ChatGPT.app/Contents/Resources/cua_node/lib/node_modules/playwright",
);
const fs = require("node:fs");
const assert = require("node:assert/strict");
const base = process.env.R2_URL || "http://127.0.0.1:2413";
const out = process.env.R2_OUTPUT || "/tmp/r2mobius";
fs.mkdirSync(out, { recursive: true });

(async () => {
  const browser = await chromium.launch({
    executablePath:
      process.env.CHROME ||
      "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
    headless: true,
    args: ["--enable-webgl", "--ignore-gpu-blocklist", "--use-angle=metal"],
  });
  const result = { errors: [], shots: [], checks: [] };
  try {
    for (const [width, height] of [[1280, 720], [1920, 1080]]) {
      const context = await browser.newContext({
        viewport: { width, height },
        deviceScaleFactor: 1,
      });
      const page = await context.newPage();
      page.on("pageerror", (error) => result.errors.push(String(error)));
      page.on("console", (message) => {
        if (/GL_INVALID|WebGL:.*INVALID/.test(message.text()))
          result.errors.push(message.text());
      });
      await page.request.post(base + "/api/world/close");
      await page.goto(base);
      await page.waitForFunction(() => globalThis.viewerR2);
      assert.equal(await page.locator("#world-label").textContent(), "Möbius");
      assert.equal(await page.locator("#map-open").isEnabled(), true);
      const opening = await page.evaluate(() => viewerR2.state);
      assert(opening.world.loaded && opening.world.scene);
      assert.deepEqual(opening.camera, opening.world.scene.camera);
      assert(opening.settings.time >= 19 && opening.settings.time < 20);
      assert.equal(opening.settings.timeFlow, true);
      // 固定开场光照，避免截图时月亮随真实等待时间升出画面。
      await page.click("#time-flow");
      await page.locator("#time").evaluate((element) => {
        element.value = "19";
        element.dispatchEvent(new Event("change"));
      });
      const settled = async () => {
        await page.waitForFunction(() => {
          const s = viewerR2.state.stream;
          return s && s.loaded === s.wanted && !s.pending && !s.nearPending && !s.failed;
        }, null, { timeout: 120000 });
        assert.equal(await page.evaluate(() => viewerR2.state.error), 0);
      };
      const shot = async (name) => {
        const path = `${out}/${width}x${height}-${name}.png`;
        await page.screenshot({ path });
        result.shots.push(path);
      };
      await settled();
      await page.waitForFunction(() => viewerR2.renderer.accumulated >= 8);
      const decoded = await page.evaluate(async () => {
        const { decodeRegion, unpack } = await import("/r2/scene/prepare.js");
        const { world } = viewerR2.state;
        const response = await fetch(`/api/world/region?world=${world.id}&x=0&z=0`);
        const region = decodeRegion(await response.arrayBuffer());
        const models = new Set(region.sections.flatMap((section) => [...unpack(section)]));
        const lantern = viewerR2.renderer.catalog.states["minecraft:sea_lantern []"];
        return {
          sections: region.sections.length,
          unknown: region.unknown,
          onlyLantern: [...models].every((model) => model === 0 || model === lantern),
          hasLantern: models.has(lantern),
        };
      });
      assert(decoded.sections > 0 && decoded.hasLantern && decoded.onlyLantern);
      assert.equal(decoded.unknown, 0);
      // 包装实际绘制入口，只记录选项，画面仍由原入口完成。
      await page.evaluate(() => {
        const r = viewerR2.renderer;
        const draw = r.draw.bind(r), photograph = r.photograph.bind(r);
        globalThis.mobiusOptions = {};
        r.draw = (camera, options) => {
          mobiusOptions.draw = options;
          return draw(camera, options);
        };
        r.photograph = (camera, options) => {
          mobiusOptions.photo = options;
          return photograph(camera, options);
        };
        viewerR2.continuous = true;
      });
      await page.waitForFunction(() => mobiusOptions.draw?.ocean === 64);
      await page.evaluate(() => { viewerR2.continuous = false; });
      await shot("mobius");
      if (width === 1280) {
        await page.locator("#photo-samples").evaluate((element) => {
          element.value = "4";
          element.dispatchEvent(new Event("change"));
        });
        const downloadEvent = page.waitForEvent("download", { timeout: 120000 });
        await page.click("#photo");
        const download = await downloadEvent;
        await download.saveAs(`${out}/mobius-photo.png`);
        assert.equal(await page.evaluate(() => mobiusOptions.photo.ocean), 64);
        await page.waitForFunction(() => document.querySelector("#photo-progress").hidden);
      }
      await page.click("#map-open");
      await page.evaluate(() => { viewerR2.map.center = [0, 87, 0]; });
      await settled();
      await shot("map");
      await page.evaluate(() => {
        viewerR2.map.center = [46, 87, 0];
        viewerR2.map.span = 160;
      });
      await page.evaluate(() => viewerR2.map.choose({
        x: innerWidth / 2,
        y: innerHeight / 2,
      }));
      await page.waitForFunction(() => viewerR2.state.map.selection);
      await page.evaluate(() => viewerR2.map.zoom({
        span: 5000,
        x: innerWidth / 2,
        y: innerHeight / 2,
      }));
      await page.waitForFunction(() =>
        viewerR2.state.map.tiles > 0 && viewerR2.state.map.overview,
      );
      await shot("chunks");
      await page.keyboard.press("Escape");
      await page.evaluate(() => viewerR2.setCamera([400, 100, 400], [0, 64, 0]));
      await page.click("#close-world");
      await page.waitForFunction((id) => viewerR2.state.world.id !== id, opening.world.id);
      const restored = await page.evaluate(() => viewerR2.state);
      assert.deepEqual(restored.camera, opening.world.scene.camera);
      assert.equal(restored.settings.time, 19);
      assert.equal(restored.settings.timeFlow, false);
      await settled();
      result.checks.push({
        viewport: [width, height],
        decoded,
        stream: await page.evaluate(() => viewerR2.state.stream),
      });
      await context.close();
    }
    assert.deepEqual(result.errors, []);
  } finally {
    await browser.close();
    fs.writeFileSync(`${out}/results.json`, JSON.stringify(result, null, 2));
  }
  console.log(JSON.stringify(result, null, 2));
})().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});
