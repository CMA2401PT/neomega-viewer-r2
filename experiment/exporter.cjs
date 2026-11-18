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
      expected = await live.evaluate(() => ({
        camera: viewerR2.state.camera,
        dimension: viewerR2.state.world.dimension ?? ".",
      }));
      const download = live.waitForEvent("download");
      await live.locator("#export-html").click();
      temporary = await mkdtemp(path.join(tmpdir(), "viewer-export-"));
      file = path.join(temporary, "snapshot.html");
      await (await download).saveAs(file);
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
    if (expected) {
      const meta = JSON.parse(await page.locator("#r2-world").textContent());
      assert.deepEqual(meta.camera, expected.camera);
      assert.deepEqual(meta.dimensions.map(dimension => dimension.id), [expected.dimension]);
    }
    assert.deepEqual(await page.evaluate(() => window.compiledTiers), ["basic"]);
    await page.locator("#map-open").click();
    assert(await page.locator("#map-hud").isVisible());
    await page.locator("#map-open").click();
    assert(await page.locator("#map-hud").isHidden());
    await page.locator('[data-tier="shading"]').click();
    await page.waitForFunction(() => window.compiledTiers.includes("shading") &&
      document.getElementById("task-status").hidden, null, { timeout: 120000 });
    assert.deepEqual(errors, []);
    assert.deepEqual(requests, []);
    console.log("通过：本地文件离线加载，基础档启动，光影按需编译");
  } finally {
    await browser.close();
    if (temporary) await rm(temporary, {recursive: true, force: true});
  }
}

check(process.argv[2]).catch(error => { console.error(error); process.exitCode = 1; });
