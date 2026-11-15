// 在真实 Chrome 中验证空白启动、近远景设置、移动复用、卸载、会话切换和活动对象。
const { chromium } = require(
  process.env.PLAYWRIGHT_MODULE ||
    "/Applications/ChatGPT.app/Contents/Resources/cua_node/lib/node_modules/playwright",
);
const fs = require("node:fs"),
  assert = require("node:assert/strict");
const base = process.env.R2_URL || "http://127.0.0.1:2413",
  fixture = process.env.R2_FIXTURE || "/tmp/neomega-r2-java-fixture",
  haven = process.env.R2_WORLD || "/Users/dai/Downloads/新天堂",
  out = process.env.R2_OUTPUT || "/tmp/viewer-r2-check";
fs.mkdirSync(out, { recursive: true });
(async () => {
  const browser = await chromium.launch({
    executablePath:
      process.env.CHROME ||
      "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
    headless: true,
    args: ["--enable-webgl", "--ignore-gpu-blocklist", "--use-angle=metal"],
  });
  const result = { errors: [], stages: [] };
  try {
    const page = await browser.newPage({
      viewport: { width: 1280, height: 720 },
      deviceScaleFactor: 1,
    });
    const requests = [];
    page.on("request", (r) => requests.push(r.url()));
    page.on("pageerror", (e) => result.errors.push(String(e)));
    page.on("console", (m) => {
      if (m.text().includes("GL_INVALID")) result.errors.push(m.text());
    });
    await page.goto(base);
    await page.waitForFunction(() => globalThis.viewerR2);
    assert.equal(await page.locator("#empty").isVisible(), true);
    assert.equal(requests.filter((s) => s.includes("/r2/assets/")).length, 0);
    const settled = async () => {
      await page.evaluate(
        () =>
          new Promise((resolve) =>
            requestAnimationFrame(() => requestAnimationFrame(resolve)),
          ),
      );
      await page.waitForFunction(
        () => {
          const s = viewerR2.state.stream;
          return (
            s && s.loaded === s.wanted && s.pending === 0 && s.nearPending === 0
          );
        },
        null,
        { timeout: 120000 },
      );
      assert.equal((await page.evaluate(() => viewerR2.state)).error, 0);
    };
    const stage = async (name) => {
      const state = await page.evaluate(() => viewerR2.state);
      const row = {
        name,
        ...state,
        gpuTimes: state.gpuTimes.slice(-30),
        frames: state.frames.slice(-30),
      };
      result.stages.push(row);
      console.log(
        JSON.stringify({ ...row, gpuTimes: undefined, frames: undefined }),
      );
    };
    const change = async (id, value) => {
      await page.locator("#" + id).evaluate((element, value) => {
        element.value = String(value);
        element.dispatchEvent(new Event("change"));
      }, value);
    };
    const pixels = () =>
      page.evaluate(() => {
        const p = viewerR2.renderer.pixels();
        let s = "";
        for (let i = 0; i < p.length; i += 8192)
          s += String.fromCharCode(...p.subarray(i, i + 8192));
        return btoa(s);
      });
    await page.click("#open-empty");
    await page.waitForFunction(() =>
      document.querySelector("#file-message").textContent.endsWith("个目录"),
    );
    await page.fill("#directory", fixture);
    await page.click("#open-directory");
    await settled();
    assert.equal((await page.evaluate(() => viewerR2.state)).stream.objects, 1);
    await page.evaluate(() =>
      viewerR2.setCamera([8.5, 3.8, 13], [8.5, 3.1, 8.5]),
    );
    await page.waitForTimeout(300);
    await page.screenshot({ path: out + "/objects.png" });
    const on = await pixels();
    await page.evaluate(() => (viewerR2.renderer.objects.enabled = false));
    await page.waitForTimeout(100);
    const off = await pixels();
    assert.notEqual(on, off, "已加载的效果没有参加射线绘制");
    await page.evaluate(() => (viewerR2.renderer.objects.enabled = true));
    await stage("java-effector");
    await change("far-radius", 16);
    await settled();
    assert.equal((await page.evaluate(() => viewerR2.state)).stream.wanted, 16);
    await stage("far-radius-16");
    await page.click("#close-world");
    await page.waitForFunction(() => !viewerR2.state.world.loaded);
    assert.equal(await page.locator("#empty").isVisible(), true);
    await change("far-radius", 48);
    const start = Date.now();
    await page.evaluate((path) => viewerR2.openWorld(path), haven);
    let first = -1;
    for (let i = 0; i < 300; i++) {
      const s = await page.evaluate(() => viewerR2.state.stream);
      if (s?.loaded > 0 && first < 0) first = Date.now() - start;
      if (s?.loaded === s?.wanted && s?.pending === 0 && s?.nearPending === 0)
        break;
      await page.waitForTimeout(50);
    }
    await settled();
    result.firstRegionMS = first;
    result.fullWindowMS = Date.now() - start;
    await page.evaluate(() =>
      viewerR2.setCamera([1280, 271, 1280], [1120, 185, 1080]),
    );
    await page.waitForTimeout(300);
    const original = await pixels();
    await stage("haven-initial");
    await page.screenshot({ path: out + "/world.png" });
    const before = requests.filter((s) =>
      s.includes("/api/world/region"),
    ).length;
    await page.evaluate(() =>
      viewerR2.setCamera([1440, 271, 1280], [1280, 185, 1080]),
    );
    await settled();
    result.moveRequests =
      requests.filter((s) => s.includes("/api/world/region")).length - before;
    assert(result.moveRequests <= 28, "移动重载了完整远景");
    await stage("moved");
    await page.evaluate(() =>
      viewerR2.setCamera([1280, 271, 1280], [1120, 185, 1080]),
    );
    await settled();
    await page.waitForTimeout(200);
    assert.equal(await pixels(), original, "返回同一位置后出现缺块或残留");
    const used = (await page.evaluate(() => viewerR2.state)).stream.usedBytes;
    await change("far-radius", 24);
    await settled();
    const reduced = await page.evaluate(() => viewerR2.state);
    assert.equal(reduced.stream.wanted, 36);
    assert(reduced.stream.usedBytes < used);
    await stage("far-radius-24");
    await change("far-radius", 48);
    await settled();
    const regionBeforeNear = requests.filter((s) =>
      s.includes("/api/world/region"),
    ).length;
    await page.evaluate(() =>
      viewerR2.setCamera([1280, 78, 1280], [1210, 70, 1200]),
    );
    await settled();
    const near8 = (await page.evaluate(() => viewerR2.state)).stream.sections;
    await change("near-radius", 16);
    await settled();
    assert(
      (await page.evaluate(() => viewerR2.state)).stream.sections === near8,
    );
    assert.equal(
      requests.filter((s) => s.includes("/api/world/region")).length,
      regionBeforeNear,
    );
    await stage("near-radius-16");
    await change("near-radius", 8);
    await settled();
    await page.evaluate(() =>
      viewerR2.setCamera([10000, 80, 10000], [9900, 70, 9900]),
    );
    await page.waitForTimeout(100);
    await page.evaluate(() =>
      viewerR2.setCamera([1280, 271, 1280], [1120, 185, 1080]),
    );
    await settled();
    await page.waitForTimeout(100);
    assert.equal(await pixels(), original, "取消远处加载后出现旧区域");
    await stage("rapid-return");
    await page.evaluate(() =>
      viewerR2.setCamera([10000, 80, 10000], [9900, 70, 9900]),
    );
    await page.waitForTimeout(50);
    await page.evaluate((path) => viewerR2.openWorld(path), fixture);
    await settled();
    const switched = await page.evaluate(() => viewerR2.state);
    assert.equal(switched.stream.sections, 1);
    assert.equal(switched.stream.objects, 1);
    await stage("switch-during-load");
    assert.deepEqual(result.errors, []);
  } catch (error) {
    console.error(JSON.stringify(result));
    throw error;
  } finally {
    await browser.close();
    fs.writeFileSync(out + "/results.json", JSON.stringify(result, null, 2));
  }
})().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});
