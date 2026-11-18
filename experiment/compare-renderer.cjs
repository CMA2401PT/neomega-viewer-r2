// 只替换渲染核心（neomega-r2 的 dist），保持来源、资源和应用设置一致，避免跨版本基线混入配置差异。
const { chromium } = require(
  process.env.PLAYWRIGHT_MODULE ||
    "/Applications/ChatGPT.app/Contents/Resources/cua_node/lib/node_modules/playwright",
);
const fs = require("node:fs");
const path = require("node:path");
const os = require("node:os");
const { execFileSync } = require("node:child_process");
const assert = require("node:assert/strict");
const root = path.resolve(__dirname, "../../..");
const core = path.join(root, "neomega-r2", "r2");
const base = process.env.R2_URL || "http://127.0.0.1:2417";
const out = process.env.R2_OUTPUT || "/tmp/r2link/ui2/performance";
const refs = (process.env.R2_COMPARE || "d964c8c,current").split(",");
const settings = {
  nearRadius: 2,
  farRadius: 32,
  resolution: "native",
  tier: "shading",
  time: 10,
  timeFlow: false,
  fog: true,
  stillAccumulation: false,
};
fs.mkdirSync(out, { recursive: true });

/** 读取一份构建好的核心目录，键为相对 /r2/ 的路径。 @param {string} dir */
function readDist(dir) {
  const files = new Map();
  for (const entry of fs.readdirSync(dir, { recursive: true, withFileTypes: true }))
    if (entry.isFile()) {
      const file = path.join(entry.parentPath, entry.name);
      files.set(path.relative(dir, file).split(path.sep).join("/"), fs.readFileSync(file));
    }
  return files;
}

/** 响应替换只影响测试浏览器，工作区和运行中的服务都不用切换文件。
 * current 读取工作区的 dist（先 npm run build）；R2_VARIANTS/<ref> 是另存的 dist；
 * 其他值是 neomega-r2 的 Git 版本，在临时 worktree 中用工作区的 TypeScript 构建。
 * 只有与当前应用接口一致的版本（四档绘制器之后）才能比较。
 * @param {string} ref
 */
function coreFiles(ref) {
  if (ref === "current") return readDist(path.join(core, "dist"));
  const variant = process.env.R2_VARIANTS && path.join(process.env.R2_VARIANTS, ref);
  if (variant && fs.existsSync(variant)) return readDist(variant);
  const tree = fs.mkdtempSync(path.join(os.tmpdir(), "r2-core-"));
  execFileSync("git", ["worktree", "add", "--detach", tree, ref], { cwd: core, stdio: "ignore" });
  // 旧版本的核心在仓库根目录。
  const source = fs.existsSync(path.join(tree, "r2/package.json")) ? path.join(tree, "r2") : tree;
  try {
    fs.symlinkSync(path.join(core, "node_modules"), path.join(source, "node_modules"), "junction");
    execFileSync(process.execPath, [path.join(source, "scripts/build.mjs")], { cwd: source, stdio: "inherit" });
    return readDist(path.join(source, "dist"));
  } finally {
    fs.rmSync(path.join(source, "node_modules"), { force: true });
    execFileSync("git", ["worktree", "remove", "--force", tree], { cwd: core, stdio: "ignore" });
  }
}

(async () => {
  const snapshots = refs.map((ref) => ({
    ref,
    files: coreFiles(ref),
  }));
  const browser = await chromium.launch({
    executablePath:
      process.env.CHROME ||
      "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
    headless: true,
    args: ["--enable-webgl", "--ignore-gpu-blocklist", "--use-angle=metal"],
  });
  const report = {
    browser: browser.version(),
    isolated: !!process.env.R2_ISOLATE_DRAW,
    settings,
    waterTime: 0,
    resolution: [1920, 1080],
    errors: [],
    runs: [],
  };
  try {
    for (const [run, snapshot] of snapshots.entries()) {
      const context = await browser.newContext({
        viewport: { width: 1920, height: 1080 },
        deviceScaleFactor: 1,
      });
      await context.addInitScript((settings) => {
        localStorage.setItem("neomega-r2-settings", JSON.stringify(settings));
        const raf = requestAnimationFrame.bind(globalThis);
        globalThis.renderComparison = { paused: false, callback: null };
        globalThis.requestAnimationFrame = (callback) => {
          if (callback.name === "frame") {
            renderComparison.callback = callback;
            if (renderComparison.paused) return 0;
          }
          return raf(callback);
        };
      }, settings);
      const page = await context.newPage();
      page.on("pageerror", (error) => report.errors.push(String(error)));
      await page.route("**/r2/**", async (route) => {
        const name = new URL(route.request().url()).pathname.slice("/r2/".length);
        if (name.startsWith("common_ui/")) return route.continue();
        const body = snapshot.files.get(name);
        if (body)
          await route.fulfill({
            body,
            contentType: name.endsWith(".js")
              ? "application/javascript"
              : "text/plain",
          });
        else await route.continue();
      });
      await page.goto(new URL("?backend=webgl",base).href);
      await page.waitForFunction(() => globalThis.viewerR2);
      report.gpu = await page.evaluate(() => {
        const gl = viewerR2.renderer.gl;
        const extension = gl.getExtension("WEBGL_debug_renderer_info");
        return gl.getParameter(extension.UNMASKED_RENDERER_WEBGL);
      });
      await page.evaluate(async (world) => {
        await viewerR2.openWorld(world);
        const time = document.querySelector("#time");
        time.value = "10";
        time.dispatchEvent(new Event("input"));
        const draw = viewerR2.renderer.draw;
        renderComparison.draw = draw;
        viewerR2.renderer.draw = (camera, options) =>
          draw(camera, { ...options, time: 0 });
        viewerR2.continuous = true;
      }, process.env.R2_WORLD || "/Users/dai/Downloads/新天堂");
      const result = {
        ref: snapshot.ref,
        views: [],
      };
      for (const [name, eye, target] of [
        ["aerial", [1280, 271, 1280], [1120, 185, 1080]],
        ["canopy", [1280, 78, 1280], [1210, 70, 1200]],
        ["building", [1180, 85, 1160], [1110, 72, 1090]],
      ]) {
        if (
          process.env.R2_VIEWS &&
          !process.env.R2_VIEWS.split(",").includes(name)
        )
          continue;
        await page.evaluate(() => {
          if (renderComparison.paused) {
            renderComparison.paused = false;
            requestAnimationFrame(renderComparison.callback);
          }
        });
        await page.evaluate(
          ({ eye, target }) => viewerR2.setCamera(eye, target),
          { eye, target },
        );
        await page.waitForTimeout(100);
        await page.waitForFunction(
          () => {
            const s = viewerR2.state.stream;
            return (
              s.loaded === s.wanted && !s.pending && !s.nearPending && !s.failed
            );
          },
          null,
          { timeout: 180000 },
        );
        if (process.env.R2_ISOLATE_DRAW) {
          // 暂停宿主帧循环，只提交固定参数的预览，区分绘制程序与页面调度的影响。
          await page.evaluate(async () => {
            renderComparison.paused = true;
            await new Promise(requestAnimationFrame);
            const r = viewerR2.renderer,
              camera = viewerR2.state.camera;
            const angle = ((10 - 6) / 24) * 2 * Math.PI;
            r.resetTiming();
            while (r.times.length < 240) {
              renderComparison.draw(camera, {
                width: 1920,
                height: 1080,
                time: 0,
                accumulate: false,
                sun: [Math.cos(angle), Math.sin(angle), 0.3],
                fog: 512,
                synchronize: true,
              });
              await new Promise(requestAnimationFrame);
            }
          });
        } else {
          await page.evaluate(() => viewerR2.renderer.resetTiming());
          await page.waitForFunction(
            () => viewerR2.renderer.times.length === 240,
            null,
            { timeout: 120000 },
          );
        }
        const state = await page.evaluate(() => ({
          ...viewerR2.state,
          accumulated: viewerR2.renderer.accumulated,
        }));
        assert.equal(state.accumulated, 0, "比较必须使用预览帧");
        assert.equal(state.error, 0);
        for (const [key, value] of Object.entries(settings))
          assert.equal(state.settings[key], value);
        const times = state.gpuTimes.slice(40).sort((a, b) => a - b);
        const view = {
          name,
          eye,
          target,
          gpuMedianMS: times[100],
          gpuP95MS: times[190],
          stream: state.stream,
          times,
        };
        result.views.push(view);
        console.log(
          JSON.stringify({
            ref: snapshot.ref,
            name,
            median: view.gpuMedianMS,
            p95: view.gpuP95MS,
          }),
        );
        await page.screenshot({
          path: path.join(out, `${run}-${snapshot.ref}-${name}.png`),
        });
        const canvas = await page.evaluate(() =>
          document.querySelector("#view").toDataURL("image/png"),
        );
        fs.writeFileSync(
          path.join(out, `${run}-${snapshot.ref}-${name}-canvas.png`),
          Buffer.from(canvas.split(",")[1], "base64"),
        );
      }
      report.runs.push(result);
      await context.close();
    }
    assert.deepEqual(report.errors, []);
  } finally {
    await browser.close();
    fs.writeFileSync(
      path.join(out, "results.json"),
      JSON.stringify(report, null, 2),
    );
  }
})().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});
