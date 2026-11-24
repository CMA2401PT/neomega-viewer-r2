// 验证真实 file:// 环境中的 worker、资源与按需编译。传入离线 HTML 或正在运行的 viewer 地址。
const assert = require("node:assert/strict");
const { mkdtemp, rm } = require("node:fs/promises");
const { tmpdir } = require("node:os");
const path = require("node:path");
const { pathToFileURL } = require("node:url");
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || "playwright");

async function check(file) {
  if (!file) throw Error("用法：node experiment/exporter.cjs <已导出的 HTML>");
  const browser = await chromium.launch({
    channel: "chrome",
    args: (process.env.R2_BROWSER_ARGS || "").split(" ").filter(Boolean),
  });
  let temporary;
  try {
    let expected;
    if (/^https?:/.test(file)) {
      const live = await browser.newPage({ acceptDownloads: true });
      await live.goto(file);
      await live.waitForFunction(() => window.viewerR2?.state.stream?.loaded > 0);
      // 在真正点击的同一事件中对照设置，时间流逝或导出期间的改动不应改变快照。
      await live.evaluate(() => document.getElementById("export-html").addEventListener("click", () => {
        const { camera, settings, world } = viewerR2.state;
        const { textureSize, ...display } = settings;
        window.expectedSnapshot = structuredClone({ camera, settings: display, dimension: world.dimension ?? "." });
      }, { capture: true, once: true }));
      const download = live.waitForEvent("download");
      await live.locator("#export-html").click();
      // 导出期间仍可改镜头。文件要保留点击瞬间的视场角。
      await live.locator("#fov").evaluate(input => {
        input.value = input.value === "80" ? "65" : "80";
        input.dispatchEvent(new Event("input", { bubbles: true }));
      });
      temporary = await mkdtemp(path.join(tmpdir(), "viewer-export-"));
      file = path.join(temporary, "snapshot.html");
      await (await download).saveAs(file);
      expected = await live.evaluate(() => window.expectedSnapshot);
      await live.close();
    }
    const context = await browser.newContext({ offline: true });
    const page = await context.newPage();
    const errors = [], requests = [];
    context.on("request", request => {
      if (/^https?:/.test(request.url())) requests.push(request.url());
    });
    page.on("websocket", socket => requests.push(socket.url()));
    page.on("pageerror", error => errors.push(String(error)));
    await page.addInitScript(() => {
      // 记录 GPU 实际收到的档位；按钮的初始选中状态无法检出后台预编译。
      window.compiledTiers = [];
      for (const method of ["createRenderPipelineAsync", "createComputePipelineAsync"]) {
        const original = GPUDevice.prototype[method];
        GPUDevice.prototype[method] = function(descriptor) {
          if (["voxel", "basic", "shading", "accumulation", "photo"].includes(descriptor.label))
            window.compiledTiers.push(descriptor.label);
          return original.call(this, descriptor);
        };
      }
    });
    await page.goto(pathToFileURL(path.resolve(file)).href);
    await page.waitForFunction(() =>
      document.getElementById("startup-status").hidden &&
      document.getElementById("loading").textContent === "已加载", null, { timeout: 120000 });
    assert.equal(await page.locator("#render-backend").textContent(), "[webGPU]");
    const meta = JSON.parse(await page.locator("#r2-world").textContent());
    if (expected) {
      assert.deepEqual(meta.camera, expected.camera);
      assert.deepEqual(meta.settings, expected.settings);
      assert.deepEqual(meta.dimensions.map(dimension => dimension.id), [expected.dimension]);
    }
    const tier = meta.settings?.tier ?? "basic";
    const compiled = [tier];
    if (meta.settings?.stillAccumulation) {
      await page.waitForFunction(() => window.compiledTiers.includes("accumulation"));
      compiled.push("accumulation");
    }
    assert.deepEqual(await page.evaluate(() => window.compiledTiers), compiled);
    assert.equal(await page.locator(`[data-tier="${tier}"]`).getAttribute("aria-pressed"), "true");
    if (meta.settings) {
      for (const [key, value] of Object.entries(meta.settings.appearance ?? {})) {
        const input = page.locator("#" + key);
        if (typeof value === "boolean") assert.equal(await input.isChecked(), value, key);
        else assert.equal(await input.inputValue(), String(value), key);
      }
      assert.equal(await page.locator("#far-radius").inputValue(), String(meta.settings.farRadius));
      assert.equal(await page.locator("#fog").isChecked(), meta.settings.fog);
      assert.equal(await page.locator("#time-flow").getAttribute("aria-pressed"), String(meta.settings.timeFlow));
    }
    await page.locator("#map-open").click();
    assert(await page.locator("#map-hud").isVisible());
    await page.locator("#map-open").click();
    assert(await page.locator("#map-hud").isHidden());
    const nextTier = tier === "shading" ? "basic" : "shading";
    await page.locator(`[data-tier="${nextTier}"]`).click();
    await page.waitForFunction(next => window.compiledTiers.includes(next) &&
      document.getElementById("task-status").hidden, nextTier, { timeout: 120000 });
    // 从离线播放器导出参数，再拖回同一个文件，检查镜头更新且世界坐标未被带入。
    await page.click("#settings-open");
    await page.click("#tab-presets");
    const download = page.waitForEvent("download");
    await page.click("#preset-export");
    const stream = await (await download).createReadStream();
    const chunks = [];
    for await (const chunk of stream) chunks.push(chunk);
    const group = JSON.parse(Buffer.concat(chunks));
    assert.equal(group.camera.position, undefined);
    const coordinates = await page.locator("#teleport input").evaluateAll(inputs => inputs.map(input => input.value));
    group.settings.appearance.fov = group.settings.appearance.fov === 73 ? 81 : 73;
    await page.evaluate(group => {
      const transfer = new DataTransfer();
      transfer.items.add(new File([JSON.stringify(group)], "镜头.json", { type: "application/json" }));
      document.getElementById("view").dispatchEvent(new DragEvent("drop", { bubbles: true, dataTransfer: transfer }));
    }, group);
    await page.waitForFunction(fov => Number(document.getElementById("fov").value) === fov, group.settings.appearance.fov);
    assert.deepEqual(await page.locator("#teleport input").evaluateAll(inputs => inputs.map(input => input.value)), coordinates);
    assert.deepEqual(errors, []);
    assert.deepEqual(requests, []);
    console.log("通过：本地文件离线加载，使用快照的镜头、布光和预览档");
  } finally {
    await browser.close();
    if (temporary) await rm(temporary, {recursive: true, force: true});
  }
}

check(process.argv[2]).catch(error => { console.error(error); process.exitCode = 1; });
