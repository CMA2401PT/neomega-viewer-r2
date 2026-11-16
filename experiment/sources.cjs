// 真实来源的打开、转换进度和截图验收；环境变量指定服务及已有样本。
const { chromium } = require(
  process.env.PLAYWRIGHT_MODULE ||
    "/Applications/ChatGPT.app/Contents/Resources/cua_node/lib/node_modules/playwright",
);
const fs = require("node:fs"), path = require("node:path"), assert = require("node:assert/strict");
const base = process.env.R2_URL || "http://127.0.0.1:2413";
const out = process.env.R2_OUTPUT || "/tmp/r2link/sources";
const samples = [
  { name: "java", source: process.env.R2_JAVA || "/Users/dai/Downloads/新天堂", edition: "java" },
  { name: "bedrock", source: process.env.R2_BEDROCK || "resource/基岩版中地图", edition: "bedrock" },
  { name: "java-structure", source: process.env.R2_STRUCTURE || "resource/结构文件/nbt/AngelWingsNPv3-74.nbt", edition: "java", convert: true },
  { name: "mcstructure", source: process.env.R2_MCSTRUCTURE || "resource/1.21.130 - 全方块展示 - 魏信者制作.mcstructure", edition: "bedrock", convert: true },
];
fs.mkdirSync(out, { recursive: true });

// 结构与 Bedrock 都使用临时副本，原样本保持可供其他验收使用。
function samplePath(sample) {
  if (!sample.convert && sample.edition === "java") return path.resolve(sample.source);
  const directory = fs.mkdtempSync(path.join(out, sample.name + "-"));
  const target = path.join(directory, path.basename(sample.source));
  fs.cpSync(sample.source, target, { recursive: true });
  return target;
}

(async () => {
  const browser = await chromium.launch({
    executablePath: process.env.CHROME || "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
    headless: true,
    args: ["--enable-webgl", "--ignore-gpu-blocklist", "--use-angle=metal"],
  });
  const result = { errors: [], samples: [] };
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 720 }, deviceScaleFactor: 1 });
    page.on("pageerror", error => result.errors.push(String(error)));
    page.on("console", message => {
      if (/GL_INVALID|WebGL:.*INVALID/.test(message.text())) result.errors.push(message.text());
    });
    await page.request.post(base + "/api/world/close");
    await page.goto(base);
    await page.waitForFunction(() => globalThis.viewerR2);
    for (const [id, value] of [["far-radius", 16], ["time", 10], ["still-samples", 0]]) {
      await page.locator("#" + id).evaluate((element, value) => {
        element.value = String(value);
        element.dispatchEvent(new Event(element.id === "time" ? "input" : "change"));
      }, value);
    }
    await page.click("#time-flow");
    await page.locator("#fog").evaluate(element => {
      element.checked = false;
      element.dispatchEvent(new Event("change"));
    });
    for (const sample of samples) {
      const source = samplePath(sample);
      await page.click("#open-world");
      await page.waitForFunction(() => document.querySelector("#file-message").textContent.endsWith("个项目"));
      await page.locator("#directory").fill(source);
      await page.click("#browse");
      await page.waitForFunction(() => document.querySelector("#file-message").textContent.endsWith("个项目"));
      await page.locator("#source-options").evaluate(element => { element.open = true; });
      await page.locator("#source-dimension").selectOption(sample.edition === "java" ? "" : "0");
      await page.locator("#source-memory").fill("512");
      await page.locator("#source-workers").fill("2");
      if (sample.convert) await page.screenshot({ path: `${out}/${sample.name}-options.png` });
      const responsePromise = page.waitForResponse(response => response.url().endsWith("/api/world/open"));
      await page.click("#open-directory");
      const response = await responsePromise;
      const messages = (await response.text()).trim().split("\n").map(line => JSON.parse(line));
      assert.equal(messages.at(-1).type, "world", JSON.stringify(messages.at(-1)));
      assert.equal(messages.at(-1).world.edition, sample.edition);
      if (sample.convert) assert(messages.some(message => message.type === "progress"));
      await page.waitForFunction(() => !document.querySelector("#files").open);
      // 来源落点给出已有内容的位置，固定斜俯视角方便比较结构与地形。
      await page.evaluate(name => {
        const p = viewerR2.state.world.position;
        if (name === "java") viewerR2.setCamera([1280,271,1280], [1120,185,1080]);
        else if (name === "bedrock") viewerR2.setCamera([60,70,80], [0,-20,0]);
        else viewerR2.setCamera([p[0]+80,p[1]+80,p[2]+100], [p[0],p[1]-20,p[2]]);
      }, sample.name);
      await page.waitForTimeout(100);
      await page.waitForFunction(() => {
        const s = viewerR2.state.stream;
        return s.loaded === s.wanted && !s.pending && !s.nearPending && !s.failed;
      }, null, { timeout: 180000 });
      await page.waitForTimeout(1600);
      const state = await page.evaluate(() => viewerR2.state);
      assert.equal(state.error, 0);
      assert(state.stream.sections > 0, sample.name + " 没有可见 section");
      await page.screenshot({ path: `${out}/${sample.name}.png` });
      await page.click("#world-name");
      await page.screenshot({ path: `${out}/${sample.name}-info.png` });
      await page.keyboard.press("Escape");
      if (sample.convert) {
        await page.evaluate(source => viewerR2.openWorld(source), source);
        assert.equal(await page.evaluate(() => viewerR2.state.world.reusedStructure), true);
      }
      result.samples.push({ name: sample.name, source, messages, state });
      console.log(sample.name, state.stream.sections, "sections");
    }
    assert.deepEqual(result.errors, []);
  } finally {
    fs.writeFileSync(`${out}/result.json`, JSON.stringify(result, null, 2));
    await browser.close();
  }
})().catch(error => { console.error(error); process.exit(1); });
