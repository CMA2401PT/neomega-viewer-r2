// 验证完整静态模型不受近景半径影响；只检查功能，不采集帧率或加载性能。
const { chromium } = require(
  process.env.PLAYWRIGHT_MODULE ||
    "/Applications/ChatGPT.app/Contents/Resources/cua_node/lib/node_modules/playwright",
);
const fs = require("node:fs"),
  assert = require("node:assert/strict");
const out = process.env.R2_OUTPUT || "/tmp/r2-static-models";
fs.mkdirSync(out, { recursive: true });
(async () => {
  const browser = await chromium.launch({
    executablePath:
      process.env.CHROME ||
      "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
    headless: true,
    args: ["--ignore-gpu-blocklist"],
  });
  const errors = [],
    result = {};
  try {
    const page = await browser.newPage({
      viewport: { width: 1280, height: 720 },
      deviceScaleFactor: 1,
    });
    page.on("pageerror", (e) => errors.push(String(e)));
    await page.goto(process.env.R2_URL || "http://127.0.0.1:2416");
    await page.waitForFunction(() => globalThis.viewerR2);
    // 应用静止时不重画；实验需要连续的 GPU 时间与帧间隔。
    await page.evaluate(() => (viewerR2.continuous = true));
    const change = async (id, value) =>
      page.locator("#" + id).evaluate((el, value) => {
        el.value = String(value);
        el.dispatchEvent(new Event("change"));
      }, value);
    const settle = async () => {
      await page.evaluate(
        () =>
          new Promise((r) =>
            requestAnimationFrame(() => requestAnimationFrame(r)),
          ),
      );
      await page.waitForFunction(
        () => {
          const s = viewerR2.state.stream;
          return (
            s &&
            s.loaded === s.wanted &&
            !s.pending &&
            !s.nearPending &&
            !s.failed
          );
        },
        null,
        { timeout: 60000 },
      );
    };
    const draw = async (fragments = true) =>
      page.evaluate(async (fragments) => {
        const r = viewerR2.renderer;
        await r.draw(viewerR2.state.camera, {
          synchronize: true,
          width: 1280,
          height: 720,
          time: 0,
          objects: false,
          fragments,
        });
        const pixels = await r.pixels();
        let s = "";
        for (let i = 0; i < pixels.length; i += 8192)
          s += String.fromCharCode(...pixels.subarray(i, i + 8192));
        return btoa(s);
      }, fragments);
    await change("far-radius", 16);
    await change("near-radius", 1);
    await page.evaluate(
      (path) => viewerR2.openWorld(path),
      process.env.R2_FIXTURE || "/tmp/neomega-r2-static-fixture",
    );
    await page.evaluate(() => viewerR2.setCamera([128, 6, 168], [128, 2, 128]));
    await settle();
    assert.equal(await page.evaluate(() => viewerR2.state.stream.objects), 0);
    const far = await draw();
    await page.screenshot({ path: out + "/far.png" });
    const without = await draw(false);
    assert.notEqual(far, without, "跨格告示牌片段没有出现在远景中");
    result.fragmentsVisible = true;
    await change("near-radius", 16);
    await settle();
    assert(
      (await page.evaluate(() => viewerR2.state.stream.objects)) > 0,
      "近景未启用特殊对象",
    );
    const near = await draw();
    assert.equal(near, far, "近景半径改变了静态形状");
    result.sameStaticPixels = true;
    await page.screenshot({ path: out + "/near.png" });
    await change("near-radius", 1);
    await settle();
    assert.equal(await page.evaluate(() => viewerR2.state.stream.objects), 0);
    assert.equal(await draw(), far, "特殊对象卸载后改变了静态外观");
    await page.evaluate(() => viewerR2.setCamera([384, 6, 168], [128, 2, 128]));
    await settle();
    const border = await page.evaluate(() => ({
      source: viewerR2.session.entries.get("0/1")?.visible,
      fragments:
        viewerR2.session.gpu.regions.get("1/1")?.stats.fragmentReferences,
    }));
    assert.equal(border.source, false);
    assert(border.fragments > 0, "可见范围外来源的片段丢失");
    result.windowEdge = border;
    await page.evaluate(() => viewerR2.setCamera([128, 6, 168], [128, 2, 128]));
    await settle();
    assert.equal(await draw(), far, "移动返回后存在缺片或旧区域");
    result.returnPixelsEqual = true;
    await page.click("#close-world");
    await page.waitForFunction(() => !!viewerR2.state.world.scene);
    assert.deepEqual(errors, []);
    result.errors = errors;
    console.log(JSON.stringify(result));
    fs.writeFileSync(out + "/results.json", JSON.stringify(result, null, 2));
  } finally {
    await browser.close();
  }
})().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});
