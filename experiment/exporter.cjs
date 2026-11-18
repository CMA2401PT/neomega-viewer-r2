// 验证真实 file:// 环境中的 worker、资源与按需编译。传入使用内置播放器导出的 HTML。
const assert = require("node:assert/strict");
const path = require("node:path");
const { pathToFileURL } = require("node:url");
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || "playwright");

async function check(file) {
  if (!file) throw Error("用法：node experiment/exporter.cjs <已导出的 HTML>");
  const browser = await chromium.launch({
    channel: "chrome",
    args: (process.env.R2_BROWSER_ARGS || "").split(" ").filter(Boolean),
  });
  try {
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
    assert.deepEqual(await page.evaluate(() => window.compiledTiers), ["basic"]);
    await page.locator("#map-open").click();
    await page.locator("#map-close").click();
    await page.locator('[data-tier="shading"]').click();
    await page.waitForFunction(() => window.compiledTiers.includes("shading") &&
      document.getElementById("task-status").hidden, null, { timeout: 120000 });
    assert.deepEqual(errors, []);
    assert.deepEqual(requests, []);
    console.log("通过：本地文件离线加载，基础档启动，光影按需编译");
  } finally {
    await browser.close();
  }
}

check(process.argv[2]).catch(error => { console.error(error); process.exitCode = 1; });
