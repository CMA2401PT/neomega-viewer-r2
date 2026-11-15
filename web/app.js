import { createRenderer } from "/r2/render/renderer.js";
import { ObjectRegistry } from "/r2/objects/registry.js";
import { enchantingBook } from "/r2/effects/enchanting-book.js";
import { WorldStream } from "./stream.js";
import { FlightControls } from "./controls.js";
import { createPicker } from "./picker.js";
const element = (id) => document.getElementById(id),
  canvas = element("view"),
  camera = { position: [0, 80, 0], yaw: -0.7, pitch: -0.32 };
const settings = {
  nearRadius: 8,
  farRadius: 48,
  resolution: "native",
  samples: 1,
  speed: 32,
  ...JSON.parse(localStorage.getItem("neomega-r2-settings") || "{}"),
};
const controls = new FlightControls(canvas, camera);
// 新对象类型在此组装；工厂来自核心的独立实现目录，主循环不包含方块分支。
const registry = new ObjectRegistry().register("enchanting_book", () =>
  enchantingBook("/r2/assets/book.png"),
);
let renderer,
  rendererPromise,
  stream,
  world = { loaded: false },
  opening = false,
  epoch = 0,
  last = performance.now(),
  statusAt = 0,
  frames = [];
const showError = (message) => {
  if (opening) return;
  element("notice").textContent = message;
  element("notice").hidden = false;
};
function setSettings() {
  element("near-value").textContent = `${settings.nearRadius} 区块半径`;
  element("far-value").textContent =
    `${settings.farRadius} 区块半径（${settings.farRadius * 16} 方块）`;
  localStorage.setItem("neomega-r2-settings", JSON.stringify(settings));
  renderer?.resetTiming();
}
for (const [id, key] of [
  ["near-radius", "nearRadius"],
  ["far-radius", "farRadius"],
  ["resolution", "resolution"],
  ["samples", "samples"],
  ["speed", "speed"],
]) {
  element(id).value = String(settings[key]);
  element(id).addEventListener("change", () => {
    settings[key] =
      key === "resolution" ? element(id).value : Number(element(id).value);
    setSettings();
  });
}
setSettings();
async function useWorld(info) {
  const version = ++epoch;
  stream?.dispose();
  stream = null;
  world = info;
  element("world-name").textContent = info.loaded ? info.name : "未打开存档";
  element("empty").hidden = info.loaded;
  element("loading").textContent = "";
  element("notice").hidden = true;
  if (!info.loaded) {
    if (renderer) {
      renderer.clear();
      renderer.gl.clearColor(0.063, 0.09, 0.114, 1);
      renderer.gl.clear(renderer.gl.COLOR_BUFFER_BIT);
    }
    return;
  }
  element("loading").textContent = "正在加载共享模型…";
  if (!renderer) renderer = await (rendererPromise ??= createRenderer(canvas));
  if (version !== epoch) return;
  camera.position = [...info.position];
  camera.yaw = -0.7;
  camera.pitch = -0.32;
  stream = new WorldStream({
    renderer,
    world: info,
    registry,
    onError: showError,
  });
  stream.update(camera.position, settings);
  frames = [];
}
async function openWorld(path) {
  opening = true;
  element("loading").textContent = "正在打开 Java 存档…";
  try {
    const response = await fetch("/api/world/open", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ path }),
    });
    if (!response.ok) throw Error(await response.text());
    await useWorld(await response.json());
  } finally {
    opening = false;
  }
}
const picker = createPicker(openWorld);
element("open-world").onclick = picker;
element("open-empty").onclick = picker;
for (const name of ["settings", "info"])
  element(name + "-open").onclick = () => element(name).showModal();
element("jump-open").onclick = () => {
  ["x", "y", "z"].forEach(
    (axis, i) =>
      (element("jump-" + axis).value = camera.position[i].toFixed(2)),
  );
  element("jump").showModal();
};
element("jump-close").onclick = () => element("jump").close();
element("jump-form").onsubmit = (event) => {
  event.preventDefault();
  camera.position = ["x", "y", "z"].map((axis) =>
    Number(element("jump-" + axis).value),
  );
  element("jump").close();
  renderer?.resetTiming();
};
element("close-world").onclick = async () => {
  const response = await fetch("/api/world/close", { method: "POST" });
  await useWorld(await response.json());
};
function frame(now) {
  requestAnimationFrame(frame);
  const dt = Math.min(0.05, (now - last) / 1000);
  frames.push(now - last);
  if (frames.length > 240) frames.shift();
  last = now;
  if (!world.loaded || !stream) return;
  controls.update(dt, Number(settings.speed));
  stream.update(camera.position, settings);
  stream.publish();
  const scale =
      settings.resolution === "native"
        ? devicePixelRatio
        : Number(settings.resolution),
    width = Math.round(innerWidth * scale),
    height = Math.round(innerHeight * scale);
  renderer.draw(camera, {
    width,
    height,
    samples: settings.samples,
    time: now / 1000,
  });
  if (now - statusAt > 300) {
    const stats = stream.stats,
      times = renderer.times.sort((a, b) => a - b),
      median = times[Math.floor(times.length / 2)],
      interval = frames.reduce((a, b) => a + b, 0) / frames.length;
    element("loading").textContent =
      stats.pending || stats.nearPending
        ? `近景对象准备 ${stats.nearPending} 区域 · 远景覆盖 ${stats.loaded}/${stats.wanted}`
        : "已加载";
    element("position").textContent = camera.position
      .map((v) => v.toFixed(1))
      .join(" / ");
    element("fps").textContent =
      `${Math.round(1000 / interval)} FPS · ${width}×${height}`;
    element("details").textContent =
      `${world.name}\n${world.path}\n\n近景半径：${settings.nearRadius} 区块\n远景半径：${settings.farRadius} 区块，覆盖 ${settings.farRadius * 32}×${settings.farRadius * 32}×512 方块\n静态模型 section：${stats.sections}\n已发布区域：${stats.loaded}/${stats.wanted}\n待更新区域：${stats.pending} · 失败：${stats.failed}\n特殊对象：${stats.objects}\n累计传输：${(stats.bytes / 1024 ** 2).toFixed(1)} MiB\n区域显存已用：${(stats.usedBytes / 1024 ** 2).toFixed(1)} MiB\nGPU 纹理分配：${(renderer.textureBytes / 1024 ** 2).toFixed(1)} MiB\nGPU 中位数：${median?.toFixed(2) || "…"} ms\n实际像素：${width}×${height} · 每像素 ${settings.samples} 条射线\n\n仅支持 Java 主世界。含水附加层与更多 NBT 外观尚未接入。`;
    statusAt = now;
  }
}
window.addEventListener("pagehide", () => {
  stream?.dispose();
  renderer?.dispose();
});
// 验收入口只读状态并允许固定相机，使用同一流式调度与绘制路径。
globalThis.viewerR2 = {
  get state() {
    return {
      world,
      settings: { ...settings },
      camera: { ...camera, position: [...camera.position] },
      stream: stream?.stats,
      gpuTimes: renderer?.times || [],
      frames: [...frames],
      error: renderer?.gl.getError() || 0,
    };
  },
  setCamera(position, target) {
    camera.position = [...position];
    const d = target.map((v, i) => v - position[i]);
    camera.yaw = Math.atan2(d[0], -d[2]);
    camera.pitch = Math.asin(d[1] / Math.hypot(...d));
    renderer?.resetTiming();
  },
  openWorld,
  close: () => element("close-world").click(),
  get renderer() {
    return renderer;
  },
  get stream() {
    return stream;
  },
};
requestAnimationFrame(frame);
await useWorld(await (await fetch("/api/world")).json());
