import { decodeTextures } from "/r2/render/textures.js";
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
  speed: 32,
  shadows: true,
  // 一天中的时刻（小时）；6 点日出，18 点日落。
  time: 10,
  timeFlow: false,
  // 远景雾遮住观察窗口的边界；关掉后远处的地形直接截断在天空前。
  fog: true,
  stillSamples: 64,
  // 照片相对渲染画面的倍数与每像素样本数。
  photoScale: 1,
  photoSamples: 32,
  texturePath: "",
  textureSize: 128,
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
  frames = [],
  // 上一次绘制时的相机、尺寸、设置与场景版本；不变且没有可见动画时跳过绘制。
  drawn = "",
  drawnAt = 0,
  // 性能实验需要每帧都绘制，才能连续采样 GPU 时间。
  continuous = false,
  // 正在拍的照片；拍照期间相机、场景和时间都停住，帧循环只推进它。
  photo = null;
const showError = (message) => {
  if (opening) return;
  element("notice").textContent = message;
  element("notice").hidden = false;
};
function setSettings() {
  element("near-value").textContent = `${settings.nearRadius} 区块半径`;
  element("far-value").textContent =
    `${settings.farRadius} 区块半径（${settings.farRadius * 16} 方块）`;
  showTime();
  localStorage.setItem("neomega-r2-settings", JSON.stringify(settings));
  renderer?.resetTiming();
}
for (const [id, key] of [
  ["near-radius", "nearRadius"],
  ["far-radius", "farRadius"],
  ["resolution", "resolution"],
  ["speed", "speed"],
  ["shadows", "shadows"],
  ["time", "time"],
  ["time-flow", "timeFlow"],
  ["fog", "fog"],
  ["still-samples", "stillSamples"],
  ["photo-scale", "photoScale"],
  ["photo-samples", "photoSamples"],
]) {
  const input = element(id),
    toggle = input.type === "checkbox";
  if (toggle) input.checked = settings[key];
  else input.value = String(settings[key]);
  input.addEventListener(id === "time" ? "input" : "change", () => {
    settings[key] = toggle
      ? input.checked
      : key === "resolution"
        ? input.value
        : Number(input.value);
    setSettings();
  });
}
// 原生倍数随系统缩放变化：Windows 100% 缩放为 1×，Mac 的 Retina 屏约为 2×。
element("resolution").options[0].textContent =
  `屏幕原生（${+devicePixelRatio.toFixed(2)}×）`;
/** 远景雾接近不透明的距离：观察窗口的半径；关闭时放到无穷远。 */
function fogDistance() {
  return settings.fog ? settings.farRadius * 16 : Infinity;
}
/** 渲染像素相对窗口 CSS 像素的倍数。 */
function renderScale() {
  return settings.resolution === "native"
    ? devicePixelRatio
    : Number(settings.resolution);
}
function showTime() {
  const minutes = Math.floor(settings.time * 60) % 1440;
  element("time-value").textContent =
    `${String(Math.floor(minutes / 60)).padStart(2, "0")}:${String(minutes % 60).padStart(2, "0")}`;
}
/** 太阳沿东西方向的圆周运动，6 点从东方（+X）升起，12 点最高，18 点在西方落下；
 * 轨道略向 +Z 倾斜，正午的影子不会正好落在脚下。
 * @param {number} hours
 */
function sunDirection(hours) {
  const angle = ((hours - 6) / 24) * 2 * Math.PI;
  return [Math.cos(angle), Math.sin(angle), 0.3];
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
  if (!renderer) renderer = await (rendererPromise ??= (async () => {
    const instance = await createRenderer(canvas);
    if (settings.texturePath || settings.textureSize !== 128) {
      try { instance.setTextures(await loadTextures(settings.texturePath, settings.textureSize)); }
      catch (error) { element("texture-status").textContent = String(error); }
    }
    return instance;
  })());
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
/** 请求由核心合成的完整资源；应用只传路径和尺寸设置。
 * @param {string} path @param {number} maxSize
 */
async function loadTextures(path, maxSize) {
  const response = await fetch("/api/textures", {
    method: "POST", headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ path, maxSize }),
  });
  if (!response.ok) throw Error(await response.text());
  return decodeTextures(await response.arrayBuffer());
}
/** 切换期间旧场景继续绘制，完成后保持相机和存档会话。 */
async function applyTextures(path) {
  const buttons = ["texture-browse", "texture-apply", "texture-reset", "texture-size"];
  for (const id of buttons) element(id).disabled = true;
  element("texture-status").textContent = "正在合成贴图与透明度数据…";
  const maxSize = Number(element("texture-size").value);
  try {
    const set = await loadTextures(path, maxSize);
    if (stream) await stream.setTextures(set);
    else renderer?.setTextures(set);
    settings.texturePath = path;
    settings.textureSize = maxSize;
    element("texture-path").value = path;
    element("texture-status").textContent = `${path ? "材质包" : "原版"} · ${set.info.side}×${set.info.side} · ${set.info.levels} 级 mip`;
    setSettings();
    drawn = "";
  } catch (error) {
    element("texture-status").textContent = String(error);
    throw error;
  } finally {
    for (const id of buttons) element(id).disabled = false;
  }
}
element("texture-path").value = settings.texturePath;
element("texture-size").value = String(settings.textureSize);
element("texture-browse").onclick = () => picker({ kind: "pack", choose: applyTextures });
element("texture-apply").onclick = () => applyTextures(element("texture-path").value).catch(showError);
element("texture-reset").onclick = () => applyTextures("").catch(showError);
element("texture-size").onchange = () => applyTextures(settings.texturePath).catch(showError);
/** 按当前画面参数开始拍照；正在拍时再按一次取消。 */
element("photo").onclick = () => {
  if (photo) return finishPhoto();
  if (!world.loaded || !renderer) return;
  const scale = renderScale() * settings.photoScale;
  photo = renderer.photograph(camera, {
    width: Math.round(innerWidth * scale),
    height: Math.round(innerHeight * scale),
    samples: settings.photoSamples,
    time: performance.now() / 1000,
    shadows: settings.shadows,
    sun: sunDirection(settings.time),
    fog: fogDistance(),
  });
  photo.name = `${world.name}-${new Date().toTimeString().slice(0, 8).replaceAll(":", "")}.png`;
};
/** 结束拍照；完成时下载 PNG，取消时直接丢弃。 */
async function finishPhoto() {
  const job = photo;
  photo = null;
  element("photo").textContent = "拍照";
  if (job.progress === 1) {
    const link = document.createElement("a");
    link.href = URL.createObjectURL(await job.png());
    link.download = job.name;
    link.click();
    // 下载由浏览器异步开始，稍后再释放地址。
    setTimeout(() => URL.revokeObjectURL(link.href), 1000);
  }
  job.dispose();
  drawn = "";
}
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
  last = now;
  if (!world.loaded || !stream) return;
  if (photo) {
    if (photo.step()) finishPhoto();
    else element("photo").textContent = `取消拍照 ${Math.floor(photo.progress * 100)}%`;
    return;
  }
  controls.update(dt, Number(settings.speed));
  // 时间流动时一天约 4 分钟。滑块跟着走，但不必每帧写入本地存储。
  if (settings.timeFlow) {
    settings.time = (settings.time + (dt * 24) / 240) % 24;
    element("time").value = String(settings.time);
    showTime();
  }
  stream.update(camera.position, settings);
  stream.publish();
  const scale = renderScale(),
    width = Math.round(innerWidth * scale),
    height = Math.round(innerHeight * scale),
    sun = sunDirection(settings.time),
    state = JSON.stringify([
      camera.position,
      camera.yaw,
      camera.pitch,
      width,
      height,
      settings.shadows,
      settings.fog,
      sun,
      renderer.version,
    ]);
  // 画面变化时出一帧预览；静止后逐帧累积样本，到上限后停笔，GPU 回到空闲。
  const changed =
    continuous ||
    state !== drawn ||
    renderer.animating(camera, { width, height });
  if (changed || renderer.accumulated < settings.stillSamples) {
    renderer.draw(camera, {
      width,
      height,
      accumulate: !changed,
      time: now / 1000,
      shadows: settings.shadows,
      sun,
      fog: fogDistance(),
    });
  }
  if (changed) {
    // 帧率只统计画面变化时连续绘制之间的间隔，静止后的第一帧不计入。
    if (now - drawnAt < 250) frames.push(now - drawnAt);
    if (frames.length > 240) frames.shift();
    drawn = state;
    drawnAt = now;
  }
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
      now - drawnAt > 300 || !frames.length
        ? renderer.accumulated < settings.stillSamples
          ? `静止累积 ${renderer.accumulated}/${settings.stillSamples} · ${width}×${height}`
          : `静止 · ${width}×${height}`
        : `${Math.round(1000 / interval)} FPS · ${width}×${height}`;
    element("details").textContent =
      `${world.name}\n${world.path}\n\n近景半径：${settings.nearRadius} 区块\n远景半径：${settings.farRadius} 区块，覆盖 ${settings.farRadius * 32}×${settings.farRadius * 32}×512 方块\n静态模型 section：${stats.sections}\n已发布区域：${stats.loaded}/${stats.wanted}\n待更新区域：${stats.pending} · 失败：${stats.failed}\n特殊对象：${stats.objects}\n累计传输：${(stats.bytes / 1024 ** 2).toFixed(1)} MiB\n区域显存已用：${(stats.usedBytes / 1024 ** 2).toFixed(1)} MiB\nGPU 纹理分配：${(renderer.textureBytes / 1024 ** 2).toFixed(1)} MiB\nGPU 中位数：${median?.toFixed(2) || "…"} ms\n实际像素：${width}×${height}\n\n仅支持 Java 主世界。含水附加层与更多 NBT 外观尚未接入。`;
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
  /** 打开后每帧都绘制，供性能实验连续采样。 */
  set continuous(value) {
    continuous = value;
  },
  openWorld,
  applyTextures,
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
