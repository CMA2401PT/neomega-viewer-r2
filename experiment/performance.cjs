// 使用应用自己的流式调度完成加载后，测量固定相机的原生 1080p GPU 成本。
const { chromium } = require(
  process.env.PLAYWRIGHT_MODULE ||
    "/Applications/ChatGPT.app/Contents/Resources/cua_node/lib/node_modules/playwright",
);
const fs = require("node:fs"),
  assert = require("node:assert/strict");
const out = process.env.R2_OUTPUT || "/tmp/viewer-r2-performance";
fs.mkdirSync(out, { recursive: true });
(async () => {
  const browser = await chromium.launch({
    executablePath:
      process.env.CHROME ||
      "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
    headless: true,
    args: ["--ignore-gpu-blocklist"],
  });
  const results = [],
    errors = [];
  try {
    const page = await browser.newPage({
      viewport: { width: 1920, height: 1080 },
      deviceScaleFactor: 1,
    });
    page.on("pageerror", (error) => errors.push(String(error)));
    await page.goto(process.env.R2_URL || "http://127.0.0.1:2413");
    await page.waitForFunction(() => globalThis.viewerR2);
    // 应用静止时不重画；实验需要连续的 GPU 时间与帧间隔。
    await page.evaluate(() => (viewerR2.continuous = true));
    await page.evaluate(
      (path) => viewerR2.openWorld(path),
      process.env.R2_WORLD || "/Users/dai/Downloads/新天堂",
    );
    for (const [name, eye, target] of [
      ["aerial", [1280, 271, 1280], [1120, 185, 1080]],
      ["canopy", [1280, 78, 1280], [1210, 70, 1200]],
      ["building", [1180, 85, 1160], [1110, 72, 1090]],
    ]) {
      await page.evaluate(
        ({ eye, target }) => viewerR2.setCamera(eye, target),
        { eye, target },
      );
      await page.waitForTimeout(100);
      await page.waitForFunction(
        () => {
          const s = viewerR2.state.stream;
          return (
            s.loaded === s.wanted && s.pending === 0 && s.nearPending === 0
          );
        },
        null,
        { timeout: 120000 },
      );
      await page.evaluate(() => viewerR2.renderer.resetTiming());
      await page.waitForFunction(
        () => viewerR2.state.gpuTimes.length === 240,
        null,
        { timeout: 60000 },
      );
      const state = await page.evaluate(() => viewerR2.state),
        times = state.gpuTimes.slice(40).sort((a, b) => a - b);
      const result = {
        name,
        eye,
        target,
        resolution: [1920, 1080],
        settings: state.settings,
        gpuMedianMS: times[Math.floor(times.length / 2)],
        gpuP95MS: times[Math.floor(times.length * 0.95)],
        stream: state.stream,
        times,
        frameIntervals: state.frames,
      };
      results.push(result);
      console.log(
        JSON.stringify({
          ...result,
          times: undefined,
          frameIntervals: undefined,
        }),
      );
      await page.screenshot({ path: out + "/" + name + ".png" });
    }
    assert.deepEqual(errors, []);
  } finally {
    await browser.close();
    fs.writeFileSync(
      out + "/results.json",
      JSON.stringify({ results, errors }, null, 2),
    );
  }
})().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});
