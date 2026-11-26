// 用真实工具栏和相机验证昼夜、手动太阳与静止累积。传入已启动的 Viewer 地址。
const assert = require("node:assert/strict");
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || "playwright");

async function check(page, address) {
  await page.addInitScript(() => {
    // 旧版允许同时保存手动太阳和时间流逝。首次载入应保留手动指定的光照。
    localStorage.setItem("neomega-r2-settings", JSON.stringify({
      timeFlow: true, stillAccumulation: false, resolution: "0.5", appearance: { manualSun: true },
    }));
    // 让真实预览帧相隔至少 100 ms，检验低帧率下的昼夜速度。
    const raf = requestAnimationFrame.bind(window);
    window.requestAnimationFrame = callback => callback.name === "frame"
      ? setTimeout(() => raf(callback), 100)
      : raf(callback);
  });
  await page.goto(address.href);
  await page.waitForFunction(() => window.viewerR2 && document.querySelector("#startup-status").hidden);
  const flow = page.locator("#time-flow"), still = page.locator("#still-accumulation");
  const manual = page.locator("#manualSun"), canvas = page.locator("#view");
  async function switches(timeFlow, stillAccumulation, manualSun) {
    assert.equal(await flow.getAttribute("aria-pressed"), String(timeFlow));
    assert.equal(await still.getAttribute("aria-pressed"), String(stillAccumulation));
    assert.equal(await manual.isChecked(), manualSun);
    const saved = await page.evaluate(() => JSON.parse(localStorage.getItem("neomega-r2-settings")));
    assert.deepEqual([saved.timeFlow, saved.stillAccumulation, saved.appearance.manualSun],
      [timeFlow, stillAccumulation, manualSun]);
  }
  await switches(false, false, true);
  await page.evaluate(() => {
    const renderer = viewerR2.renderer, draw = renderer.draw.bind(renderer);
    window.previews = [];
    renderer.draw = (camera, options) => {
      const rendered = draw(camera, options);
      if (rendered && viewerR2.state.settings.timeFlow) {
        previews.push({ clock: viewerR2.state.settings.time, seconds: options.time, sun: options.sun });
      }
      return rendered;
    };
  });
  const clock = () => page.evaluate(() => viewerR2.state.settings.time);
  async function move() {
    const start = await page.evaluate(() => viewerR2.state.camera.position);
    await canvas.focus();
    await page.keyboard.down("w");
    await page.waitForFunction(start =>
      viewerR2.state.camera.position.some((value, i) => value !== start[i])
      && viewerR2.renderer.accumulated === 0 && document.querySelector("#fps").textContent.endsWith(" FPS"), start);
    await page.keyboard.up("w");
  }

  await flow.click();
  await switches(true, false, false);
  await page.waitForFunction(() => previews.length >= 4 && /\d FPS$/.test(document.querySelector("#fps").textContent));
  const frames = await page.evaluate(() => previews.slice(-4));
  const first = frames[0], last = frames.at(-1);
  assert.notDeepEqual(first.sun, last.sun);
  assert.ok(Math.abs((last.clock - first.clock + 24) % 24 - (last.seconds - first.seconds) / 10) < 1e-6);
  const movingClock = await clock();
  await move();
  assert.notEqual(await clock(), movingClock);

  await still.click();
  await switches(false, true, false);
  const frozen = await clock();
  await page.waitForFunction(() => viewerR2.renderer.accumulated >= 3);
  await move();
  await page.waitForFunction(() => viewerR2.renderer.accumulated >= 3 &&
    document.querySelector("#fps").textContent.startsWith("静止（累积"));
  assert.equal(await clock(), frozen);

  await still.click();
  await switches(false, false, false);
  await move();
  await page.waitForFunction(() => document.querySelector("#fps").textContent === "静止");
  assert.equal(await page.evaluate(() => viewerR2.renderer.accumulated), 0);
  assert.equal(await clock(), frozen);

  await still.click();
  await flow.click();
  await switches(true, false, false);
  await page.click("#settings-open");
  await page.click("#tab-light");
  await manual.check();
  await switches(false, false, true);
  const manualClock = await clock();
  await page.click("#settings-close");
  await still.click();
  await switches(false, true, true);
  await page.waitForFunction(() => viewerR2.renderer.accumulated >= 3);
  assert.equal(await clock(), manualClock);
  await flow.click();
  await switches(true, false, false);
  await flow.click();
  await switches(false, false, false);
}

(async () => {
  const address = new URL(process.argv[2] || "http://127.0.0.1:2412");
  const browser = await chromium.launch({ channel: "chrome", executablePath: process.env.CHROME });
  try {
    const page = await browser.newPage({ viewport: { width: 600, height: 420 } });
    await check(page, address);
    await page.close();
    console.log("昼夜速度、移动与静止、开关互斥和旧设置恢复通过");
  } finally {
    await browser.close();
  }
})().catch(error => { console.error(error); process.exitCode = 1; });
