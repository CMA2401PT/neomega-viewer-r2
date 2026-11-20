// 从已打开同一存档的 Viewer 逐个采集 WebGPU 时间。重复传入同一地址即可做复测。
const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || "playwright");

const [file, ...urls] = process.argv.slice(2);
const views = process.env.R2_VIEWS_FILE
  ? JSON.parse(fs.readFileSync(process.env.R2_VIEWS_FILE, "utf8"))
  : [
      { name: "高空", eye: [1280, 271, 1280], target: [1120, 185, 1080] },
      { name: "树冠", eye: [1280, 78, 1280], target: [1210, 70, 1200] },
      { name: "建筑", eye: [1180, 85, 1160], target: [1110, 72, 1090] },
    ];
const modes = process.env.R2_EFFECTS === "1"
  ? ["累积", "太阳辉光", "云影", "薄雾", "全部开启", "累积"]
  : ["光影", "累积"];

/** 让实际会话把当前可见区域全部发布到 GPU，再开始计时。 */
async function loadView({ eye, target }) {
  const viewer = globalThis.viewerR2;
  viewer.setCamera(eye, target);
  viewer.session.focus(eye, { nearRadius: 2, farRadius: 32 });
  const start = performance.now();
  for (;;) {
    viewer.session.publish();
    const stats = viewer.session.stats;
    if (stats.failed || performance.now() - start > 180000)
      throw Error("场景未就绪 " + JSON.stringify(stats));
    if (stats.wanted && stats.loaded === stats.wanted && !stats.pending && !stats.nearPending) {
      if (!stats.sections) throw Error("该视角没有方块，请检查存档和坐标");
      return stats;
    }
    await new Promise(resolve => setTimeout(resolve, 10));
  }
}

/** 先画固定时刻的光影预览，使每次累积都从同一轮次和水面状态开始。 */
async function measure(mode) {
  const viewer = globalThis.viewerR2;
  const renderer = viewer.renderer;
  const camera = viewer.state.camera;
  const options = {
    width: 1920, height: 1080, synchronize: true, tier: "shading",
    time: 0, sun: [0.5, Math.sqrt(3) / 2, 0.3], autoSunColor: false, fog: 512,
    airExtinction: mode === "薄雾" || mode === "全部开启" ? -Math.log(0.8) / 100 : 0,
    cloudShadows: mode === "云影" || mode === "全部开启",
    solarHaze: mode === "太阳辉光" || mode === "全部开启",
    accumulate: false,
  };
  await renderer.draw(camera, options);
  options.accumulate = mode !== "光影";
  for (let frame = 0; frame < 40; frame++) {
    if (frame === 8) renderer.resetTiming();
    if (!await renderer.draw(camera, options)) throw Error("GPU 未接受绘制");
  }
  // 时间戳另行读回，GPU 已绘制完最后一帧时读回回调可能仍在排队。
  const end = performance.now() + 5000;
  while (renderer.times.length < 32 && performance.now() < end)
    await new Promise(resolve => setTimeout(resolve, 1));
  if (renderer.times.length !== 32 || renderer.accumulated !== (options.accumulate ? 40 : 0))
    throw Error("GPU 时间戳或累积轮数与提交帧数不符");
  return { mode, camera, options, timesMS: renderer.times };
}

async function main() {
  if (!file || !urls.length)
    throw Error("用法：node experiment/webgpu-timing.cjs 结果.json Viewer地址 [Viewer地址 ...]");
  const browser = await chromium.launch({
    channel: "chrome",
    executablePath: process.env.CHROME,
    args: process.platform === "darwin" ? ["--ignore-gpu-blocklist", "--use-angle=metal"] : [],
  });
  const report = {
    platform: os.platform(), osRelease: os.release(), cpu: os.cpus()[0]?.model,
    browser: browser.version(), assets: process.env.R2_ASSETS_URL || "各版本内置", views, runs: [],
  };
  try {
    for (const url of urls) {
      const context = await browser.newContext({ viewport: { width: 960, height: 540 }, deviceScaleFactor: 1 });
      const page = await context.newPage();
      if (process.env.R2_ASSETS_URL) {
        // 从指定版本取整套材质和模型；页面和着色器仍来自本次测量的 Viewer。
        await page.route("**/r2/assets/**", async route => {
          const asset = new URL(route.request().url()).pathname;
          const response = await route.fetch({ url: new URL(asset, process.env.R2_ASSETS_URL).href });
          assert.ok(response.ok(), asset);
          await route.fulfill({ response });
        });
      }
      await page.addInitScript(() => {
        localStorage.setItem("neomega-r2-settings", JSON.stringify({
          nearRadius: 2, farRadius: 32, resolution: "native", tier: "shading",
          time: 10, timeFlow: false, stillAccumulation: false, accumulationFilter: "off", fog: true,
        }));
        // 宿主和实验不能同时绘制。保留启动界面及浏览器自身的刷新。
        const raf = requestAnimationFrame.bind(window);
        window.requestAnimationFrame = callback => callback.name === "frame" ? 0 : raf(callback);
      });
      const address = new URL(url);
      address.searchParams.set("backend", "webgpu");
      await page.goto(address.href);
      await page.waitForFunction(() => !!globalThis.viewerR2, null, { timeout: 120000 });
      const info = await page.evaluate(async () => {
        const { renderer, state } = globalThis.viewerR2;
        const adapter = await navigator.gpu.requestAdapter();
        if (renderer.backend !== "webgpu" || !adapter?.features.has("timestamp-query"))
          throw Error("需要能提供 GPU 时间戳的 WebGPU 设备");
        if (!state.world.loaded || state.world.scene) throw Error("请用 -world 打开测量存档");
        await renderer.compile("accumulation");
        const { vendor, architecture, device, description } = adapter.info;
        return { world: state.world.name, dimension: state.world.dimension, vendor, architecture, device, description };
      });
      const run = { url, ...info, measurements: [] };
      report.runs.push(run);
      for (const view of views) {
        const stream = await page.evaluate(loadView, view);
        for (const mode of modes) {
          const sample = await page.evaluate(measure, mode);
          const sorted = [...sample.timesMS].sort((a, b) => a - b);
          const medianMS = (sorted[15] + sorted[16]) / 2;
          const p95MS = sorted[30];
          run.measurements.push({ view: view.name, stream, ...sample, medianMS, p95MS });
          console.log(url, view.name, mode, medianMS.toFixed(2), "ms");
        }
      }
      await context.close();
    }
  } finally {
    await browser.close();
    fs.writeFileSync(file, JSON.stringify(report, null, 2) + "\n");
  }
}
main().catch(error => { console.error(error); process.exitCode = 1; });
