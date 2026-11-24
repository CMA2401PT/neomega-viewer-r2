// 真实 Chrome 的界面截图和相机拾取验收；两个视口分别使用全新的浏览器设置。
const { chromium } = require(
  process.env.PLAYWRIGHT_MODULE ||
    "/Applications/ChatGPT.app/Contents/Resources/cua_node/lib/node_modules/playwright",
);
const fs = require("node:fs");
const assert = require("node:assert/strict");
const base = process.env.R2_URL || "http://127.0.0.1:2413";
const fixture = process.env.R2_FIXTURE || "/tmp/neomega-r2-java-fixture";
const out = process.env.R2_OUTPUT || "/tmp/r2ui";
fs.mkdirSync(out + "/shots", { recursive: true });

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
    for (const [width, height] of [
      [1280, 720],
      [1920, 1080],
    ]) {
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
      // 世界内容经 R2Link 到达：按类型统计收到与发出的消息。
      const frames = { sent: [], received: [] };
      page.on("websocket", (ws) => {
        const type = (payload) =>
          typeof payload === "string" ? "" : JSON.parse(payload.subarray(4, 4 + payload.readUInt32LE(0))).type;
        ws.on("framesent", ({ payload }) => frames.sent.push(type(payload)));
        ws.on("framereceived", ({ payload }) => frames.received.push(type(payload)));
      });
      const count = (list, type) => list.filter((t) => t === type).length;
      await page.request.post(base + "/api/world/close");
      await page.goto(base);
      await page.waitForFunction(
        () => globalThis.viewerR2 && viewerR2.renderer.times.length > 0,
      );
      const shot = async (name) => {
        const path = `${out}/shots/${width}x${height}-${name}.png`;
        await page.screenshot({ path });
        result.shots.push(path);
      };
      const change = async (id, value) =>
        page.locator("#" + id).evaluate((el, value) => {
          el.value = String(value);
          el.dispatchEvent(new Event("change"));
        }, value);
      const settled = async () => {
        await page.waitForTimeout(100);
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
          { timeout: 120000 },
        );
        assert.equal(await page.evaluate(() => viewerR2.state.error), 0);
      };
      await settled();
      await shot("mobius");
      await page.click("#open-world");
      await page.waitForFunction(() =>
        document.querySelector("#file-message").textContent.endsWith("个项目"),
      );
      await shot("picker");
      await page.keyboard.press("Escape");
      assert.deepEqual(
        await page.evaluate(() => {
          const s = viewerR2.state.settings;
          return [
            s.nearRadius,
            s.farRadius,
            s.tier,
            s.timeFlow,
            s.resolution,
            s.photoSupersample, s.exportSupersample, s.superResolution,
          ];
        }),
        [2, 32, "shading", true, "native", 1, 1, "off"],
      );
      // 开启静止累积会暂停昼夜，照片和地图拾取由此使用固定光照。
      await page.click("#still-accumulation");
      await page.locator("#time").evaluate(el => { el.value = "10"; el.dispatchEvent(new Event("input")); });
      if (width === 1280) {
        await page.evaluate(path => viewerR2.openWorld(path), fixture);
        await settled();
        const picks = await page.evaluate(async () => {
          const r = viewerR2.renderer,
            pixel = { width: 101, height: 101, x: 50, y: 50 };
          const top = {
            position: [10.5, 10000, 8.5],
            yaw: 0,
            pitch: -Math.PI / 2,
            ortho: 16,
          };
          const side = {
            position: [12, 2.5, 8.5],
            yaw: -Math.PI / 2,
            pitch: 0,
          };
          const before = r.accumulated;
          const ortho = await r.pick(top, pixel),
            perspective = await r.pick(side, pixel);
          const miss = await r.pick(
            { ...top, position: [-1000, 10000, -1000] },
            pixel,
          );
          return {
            ortho,
            perspective,
            miss,
            accumulationPreserved: r.accumulated >= before,
            error: viewerR2.state.error,
          };
        });
        assert.deepEqual(picks.ortho.position, [10, 2, 8]);
        assert.deepEqual(picks.ortho.normal, [0, 1, 0]);
        assert.deepEqual(picks.perspective.position, [10, 2, 8]);
        assert.deepEqual(picks.perspective.normal, [1, 0, 0]);
        assert.equal(picks.miss, null);
        assert.equal(picks.error, 0);
        assert(picks.accumulationPreserved);
        result.checks.push({ picks });
        await change("resolution", "native-0.5");
        await change("photo-scale", 1);
        const downloadEvent = page.waitForEvent("download");
        await page.click("#photo");
        await page.waitForFunction(() => parseInt(document.querySelector("#photo-progress").textContent) >= 1, null, {timeout: 90000});
        await page.getByRole("button", {name: "保存照片", exact: true}).click();
        const download = await downloadEvent;
        await download.saveAs(out + "/fixture-photo.png");
        await page.waitForFunction(
          () => document.querySelector("#photo-progress").hidden,
        );
        assert.equal(
          fs
            .readFileSync(out + "/fixture-photo.png")
            .subarray(1, 4)
            .toString(),
          "PNG",
        );
        await page.click("#photo");
        await page.keyboard.press("Escape");
        assert(await page.locator("#photo-progress").isHidden());
        await change("resolution", "native");
        result.checks.push({ photoDownload: true, photoCancel: true });
      }
      // 已知测试存档在 (10,2,8) 有石英砖，两种视口都能稳定复现地图拾取。
      const screenshotWorld = process.env.R2_WORLD || fixture;
      if (width !== 1280 || screenshotWorld !== fixture) await page.evaluate(path => viewerR2.openWorld(path), screenshotWorld);
      await settled();
      const resetCamera = () => page.evaluate(() => viewerR2.setCamera([10.5, 5, 8.5], [10.5, 2, 7]));
      if (screenshotWorld === fixture) await resetCamera();
      else await page.evaluate(() => viewerR2.setCamera([1280, 271, 1280], [1120, 185, 1080]));
      await page.waitForTimeout(1500);
      await shot("world");
      await page.click("#settings-open");
      await page.waitForTimeout(100);
      await shot("settings");
      await page.click("#settings-close");
      await page.click("#world-name");
      await page.waitForTimeout(100);
      await shot("info");
      await page.click("#eye");
      await shot("hidden-hud");
      assert.equal(await page.locator("#hud").isVisible(), false);
      await page.click("#eye");
      if (screenshotWorld !== fixture) {
        await page.evaluate(path => viewerR2.openWorld(path), fixture);
        await settled();
        await resetCamera();
      }
      const main = await page.evaluate(() => viewerR2.state.camera);
      const chunkRequests = count(frames.sent, "chunks");
      assert(chunkRequests > 0, "未通过 R2Link 请求区块占用图");
      await page.click("#map-open");
      await settled();
      await page.waitForTimeout(1000);
      await page.mouse.click(width / 2, height / 2);
      await page.waitForFunction(() => viewerR2.state.map.selection);
      assert(
        await page.evaluate(() => viewerR2.state.map.selection),
        "地图未拾取到方块",
      );
      await shot("map-ray");
      assert.deepEqual(await page.evaluate(() => viewerR2.state.camera), main);
      await page.waitForFunction(() => viewerR2.renderer.accumulated > 2);
      result.checks.push({
        viewport: [width, height],
        map: await page.evaluate(() => viewerR2.state.map),
      });
      if (width === 1280) {
        // 主相机和地图先移开，让石英砖所在的区域退出窗口，再点击尚未加载的旧位置。
        await page.evaluate(() => {
          viewerR2.setCamera([2048, 5, 8.5], [2048, 2, 7]);
          viewerR2.map.center = [2048, 5, 8.5];
        });
        await settled();
        const pending = await page.evaluate(() => {
          viewerR2.map.center = [10.5, 3, 8.5];
          viewerR2.map.choose({ x: innerWidth / 2, y: innerHeight / 2 });
          return viewerR2.state.map.pending;
        });
        assert(pending, "未加载区域未登记拾取重试");
        await settled();
        await page.waitForFunction(() => !viewerR2.state.map.pending && viewerR2.state.map.selection);
        assert(
          await page.evaluate(() => viewerR2.state.map.selection),
          "发布后没有重新拾取",
        );
        assert.deepEqual(
          await page.evaluate(() => viewerR2.session.center),
          [128, 0, 0],
        );
        const selection = await page.evaluate(
          () => viewerR2.state.map.selection,
        );
        await page.click("#map-jump");
        assert.equal(await page.evaluate(() => viewerR2.state.map.open), false);
        assert.deepEqual(
          await page.evaluate(() => viewerR2.state.camera.position),
          selection.position.map((v, i) => v + (i === 1 ? 2 : 0.5)),
        );
        result.checks.push({
          pendingRetry: true,
          nearCenterRetained: true,
          mapJump: true,
        });
        await resetCamera();
        await page.click("#map-open");
        await settled();
      }
      // 光标缩放须保持水平面锚点，右键旋转不能移动主相机。
      const anchor = await page.evaluate(() =>
        viewerR2.map.point({ x: 400, y: 250 }),
      );
      await page.mouse.move(400, 250);
      await page.mouse.wheel(0, 100);
      await page.waitForTimeout(150);
      const anchored = await page.evaluate(() =>
        viewerR2.map.point({ x: 400, y: 250 }),
      );
      anchor.forEach((v, i) => assert(Math.abs(v - anchored[i]) < 1e-6));
      await page.mouse.down({ button: "right" });
      await page.mouse.move(460, 280, { steps: 6 });
      await page.mouse.up({ button: "right" });
      assert.deepEqual(await page.evaluate(() => viewerR2.state.camera), main);
      await page.evaluate(() =>
        viewerR2.map.zoom({
          span: 5000,
          x: innerWidth / 2,
          y: innerHeight / 2,
        }),
      );
      await page.waitForTimeout(300);
      await shot("map-chunks");
      assert(await page.evaluate(() => viewerR2.state.map.overview));
      assert((await page.evaluate(() => viewerR2.state.map.tiles)) > 0);
      assert.equal(count(frames.sent, "chunks"), chunkRequests, "同一场景缩放应复用区块占用图");
      await page.mouse.click(width / 2, height / 2);
      await page.waitForTimeout(150);
      assert.equal(
        await page.evaluate(() => viewerR2.state.map.overview),
        false,
      );
      await page.keyboard.press("Escape");
      assert.deepEqual(await page.evaluate(() => viewerR2.state.camera), main);
      assert.equal(await page.evaluate(() => viewerR2.state.error), 0);
      await page.setViewportSize({ width: 420, height: 800 });
      await page.waitForTimeout(100);
      // 竖屏也可以直接使用：底栏折成两行，坐标输入仍然可见。
      assert(await page.locator("#teleport").isVisible());
      await shot("portrait-420x800");
      await context.close();
    }
    assert.deepEqual(result.errors, []);
  } finally {
    await browser.close();
    fs.writeFileSync(out + "/results.json", JSON.stringify(result, null, 2));
  }
  console.log(JSON.stringify(result, null, 2));
})().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});
