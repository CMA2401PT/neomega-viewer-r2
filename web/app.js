// 应用组装存档会话、飞行/地图视图与同一射线帧循环；UI 事件和投影分别放在独立模块。
import { decodeTextures } from "/r2/render/textures.js";
import { createRenderer } from "/r2/render/renderer.js";
import { ObjectRegistry } from "/r2/objects/registry.js";
import { enchantingBook } from "/r2/effects/enchanting-book.js";
import { WorldStream } from "./stream.js";
import { FlightControls } from "./controls.js";
import { bindMovePad } from "./movepad.js";
import { createPicker } from "./picker.js";
import { createHUD, element, closePopovers } from "./hud.js";
import { drawAxes } from "./overlay.js";
import { FullscreenMap } from "./map.js";

const canvas = element("view");
const camera = { position: [0, 80, 0], yaw: -0.7, pitch: -0.32 };
/** @type {import("./hud.js").ViewerSettings} */
const settings = {
  nearRadius: 2,
  farRadius: 32,
  resolution: "native",
  speed: 32,
  shadows: true,
  time: 10,
  timeFlow: true,
  fog: true,
  stillSamples: 64,
  photoScale: 1,
  photoSamples: 32,
  texturePath: "",
  textureSize: 128,
  ...JSON.parse(localStorage.getItem("neomega-r2-settings") || "{}"),
};
const controls = new FlightControls(canvas, camera);
bindMovePad(element("movepad"), controls);
const registry = new ObjectRegistry().register("enchanting_book", () =>
  enchantingBook("/r2/assets/book.png"),
);
let renderer,
  map,
  stream,
  world = { loaded: false },
  last = performance.now(),
  statusAt = 0;
let frames = [],
  drawn = "",
  drawnAt = 0,
  continuous = false,
  photo = null;
const hud = createHUD({
  settings,
  jump: (position) => {
    camera.position = position;
  },
  changed: saveSettings,
});

/** 设置持久化不参与逐帧时间推进，避免连续写入浏览器存储。 */
function saveSettings() {
  hud.settings();
  localStorage.setItem("neomega-r2-settings", JSON.stringify(settings));
  renderer?.resetTiming();
  drawn = "";
}
/** 渲染尺寸跟随原生屏幕像素或用户选择的固定倍数。 */
function renderScale() {
  return settings.resolution === "native"
    ? devicePixelRatio
    : Number(settings.resolution);
}
/** 太阳从东方升起，轨道略向南倾斜，使正午仍有可辨别的侧影。
 * @param {number} hours
 */
function sunDirection(hours) {
  const angle = ((hours - 6) / 24) * 2 * Math.PI;
  return [Math.cos(angle), Math.sin(angle), 0.3];
}
/** 所有会话共用流式路径，内置场景额外提供开场取景与时间。
 * @param {{loaded:boolean,id:number,name:string,path:string,position:number[],scene?:{ocean:number,camera:{position:number[],yaw:number,pitch:number},time:number}}} info
 */
async function useWorld(info) {
  if (photo) await finishPhoto(false);
  stream?.dispose();
  stream = null;
  world = info;
  closePopovers();
  hud.world(info);
  map.setWorld(info).catch((error) => hud.error(String(error)));
  camera.position = [...(info.scene?.camera.position || info.position)];
  camera.yaw = info.scene?.camera.yaw ?? -0.7;
  camera.pitch = info.scene?.camera.pitch ?? -0.32;
  if (info.scene) {
    settings.time = info.scene.time;
    hud.settings();
  }
  stream = new WorldStream({
    renderer,
    world: info,
    registry,
    onError: (message) => hud.error(message),
  });
  stream.update(camera.position, settings);
  frames = [];
  drawn = "";
}
/** 目录选择器与验收入口共用同一存档打开操作。
 * @param {string} path
 */
async function openWorld(path) {
  const response = await fetch("/api/world/open", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ path }),
  });
  if (!response.ok) throw Error(await response.text());
  await useWorld(await response.json());
}
saveSettings();
renderer = await createRenderer(canvas);
map = new FullscreenMap({
  renderer,
  camera,
  jump: (position) => {
    camera.position = position;
  },
});

const picker = createPicker(openWorld);
element("open-world").onclick = () => {
  closePopovers();
  picker();
};
element("close-world").onclick = async () => {
  const response = await fetch("/api/world/close", { method: "POST" });
  if (!response.ok) return hud.error(await response.text());
  await useWorld(await response.json());
};
/** 核心负责材质包合成，应用只传入路径与尺寸上限。
 * @param {string} path @param {number} maxSize
 */
async function loadTextures(path, maxSize) {
  const response = await fetch("/api/textures", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ path, maxSize }),
  });
  if (!response.ok) throw Error(await response.text());
  return decodeTextures(await response.arrayBuffer());
}
/** 旧场景持续显示，贴图和透明度目录准备好后一起切换。
 * @param {string} path
 */
async function applyTextures(path) {
  const buttons = [
    "texture-browse",
    "texture-apply",
    "texture-reset",
    "texture-size",
  ];
  for (const id of buttons) element(id).disabled = true;
  element("texture-status").textContent = "正在合成贴图与透明度数据…";
  const maxSize = Number(element("texture-size").value);
  try {
    const set = await loadTextures(path, maxSize);
    if (stream) await stream.setTextures(set);
    else renderer.setTextures(set);
    settings.texturePath = path;
    settings.textureSize = maxSize;
    element("texture-path").value = path;
    element("texture-status").textContent =
      `${path ? "材质包" : "原版"} · ${set.info.side}×${set.info.side} · ${set.info.levels} 级 mip`;
    saveSettings();
  } catch (error) {
    element("texture-status").textContent = String(error);
    throw error;
  } finally {
    for (const id of buttons) element(id).disabled = false;
  }
}
element("texture-path").value = settings.texturePath;
element("texture-size").value = String(settings.textureSize);
element("texture-browse").onclick = () =>
  picker({ kind: "pack", choose: applyTextures });
element("texture-apply").onclick = () =>
  applyTextures(element("texture-path").value).catch((error) =>
    hud.error(String(error)),
  );
element("texture-reset").onclick = () =>
  applyTextures("").catch((error) => hud.error(String(error)));
element("texture-size").onchange = () =>
  applyTextures(settings.texturePath).catch((error) =>
    hud.error(String(error)),
  );

// 拍照使用独立缓冲，不把 HUD 或标记写入照片。
element("photo").onclick = () => {
  if (photo) return finishPhoto(false);
  const scale = renderScale() * settings.photoScale;
  photo = renderer.photograph(camera, {
    width: Math.round(innerWidth * scale),
    height: Math.round(innerHeight * scale),
    samples: settings.photoSamples,
    time: performance.now() / 1000,
    shadows: settings.shadows,
    sun: sunDirection(settings.time),
    ...(world.scene ? { ocean: world.scene.ocean } : {}),
    fog: settings.fog ? settings.farRadius * 16 : Infinity,
  });
  photo.name = `${world.name || "天空"}-${new Date().toTimeString().slice(0, 8).replaceAll(":", "")}.png`;
  element("photo").setAttribute("aria-pressed", "true");
  element("photo").title = element("photo").ariaLabel = "取消拍照";
  element("photo-progress").hidden = false;
};
/** 完成后下载，取消时直接释放照片缓冲；编码期间继续冻结场景。
 * @param {boolean} download
 */
async function finishPhoto(download) {
  const job = photo;
  job.finishing = true;
  if (download) {
    const link = document.createElement("a");
    link.href = URL.createObjectURL(await job.png());
    link.download = job.name;
    link.click();
    setTimeout(() => URL.revokeObjectURL(link.href), 1000);
  }
  job.dispose();
  photo = null;
  drawn = "";
  element("photo").setAttribute("aria-pressed", "false");
  element("photo").title = element("photo").ariaLabel = "拍照";
  element("photo-progress").hidden = true;
}

/** 同一帧循环消费当前视图的相机；世界、光照和累积仍使用同一条绘制路径。
 * @param {number} now
 */
function frame(now) {
  requestAnimationFrame(frame);
  const dt = Math.min(0.05, (now - last) / 1000);
  last = now;
  if (photo) {
    if (photo.finishing) return;
    if (photo.step()) finishPhoto(true);
    else
      element("photo-progress").textContent =
        `${Math.floor(photo.progress * 100)}% · 再按取消`;
    return;
  }
  const view = map.frame(settings);
  controls.enabled = view.objects;
  controls.update(dt, Number(settings.speed));
  if (settings.shadows && settings.timeFlow) {
    settings.time = (settings.time + (dt * 24) / 240) % 24;
    hud.settings();
  }
  stream?.update(view.center, { ...settings, nearPosition: camera.position });
  stream?.publish();
  map.refresh(stream);
  const scale = renderScale(),
    width = Math.round(innerWidth * scale),
    height = Math.round(innerHeight * scale);
  const sun = sunDirection(settings.time),
    objects = view.objects;
  const state = JSON.stringify([
    view,
    width,
    height,
    settings.shadows,
    settings.fog,
    sun,
    renderer.version,
    objects,
  ]);
  const changed =
    continuous ||
    state !== drawn ||
    (objects && renderer.animating(view.camera, { width, height }));
  let rendered = false;
  if (
    !view.overview &&
    (changed || renderer.accumulated < settings.stillSamples)
  ) {
    renderer.draw(view.camera, {
      width,
      height,
      accumulate: !changed,
      time: now / 1000,
      shadows: settings.shadows,
      sun,
      ...(world.scene ? { ocean: world.scene.ocean } : {}),
      fog: view.fog,
      objects,
    });
    rendered = true;
  }
  if (changed) {
    if (now - drawnAt < 250) frames.push(now - drawnAt);
    if (frames.length > 240) frames.shift();
    drawn = state;
    drawnAt = now;
  }
  if (now - statusAt > 100) {
    hud.position(camera.position);
    drawAxes(element("axes"), camera);
    element("resolution-value").textContent = `${width}×${height}`;
    const interval = frames.reduce((a, b) => a + b, 0) / frames.length;
    element("fps").textContent = !changed
      ? rendered
        ? `累积 ${renderer.accumulated}/${settings.stillSamples}`
        : "静止"
      : `${Math.round(1000 / interval) || "—"} FPS`;
    if (stream) {
      const stats = stream.stats,
        times = renderer.times.sort((a, b) => a - b),
        median = times[Math.floor(times.length / 2)];
      hud.loading(stats);
      hud.details([
        ["近景半径", `${settings.nearRadius} 区块`],
        ["远景半径", `${settings.farRadius} 区块`],
        [
          "窗口覆盖",
          `${settings.farRadius * 32}×${settings.farRadius * 32}×512`,
        ],
        ["静态 section", String(stats.sections)],
        ["已发布区域", `${stats.loaded}/${stats.wanted}`],
        ["待更新 / 失败", `${stats.pending} / ${stats.failed}`],
        ["特殊对象", String(stats.objects)],
        ["累计传输", `${(stats.bytes / 1024 ** 2).toFixed(1)} MiB`],
        ["区域显存", `${(stats.usedBytes / 1024 ** 2).toFixed(1)} MiB`],
        ["GPU 纹理", `${(renderer.textureBytes / 1024 ** 2).toFixed(1)} MiB`],
        ["GPU 中位数", `${median?.toFixed(2) || "…"} ms`],
        ["实际像素", `${width}×${height}`],
      ]);
    }
    statusAt = now;
  }
}

if (settings.texturePath || settings.textureSize !== 128)
  await applyTextures(settings.texturePath).catch((error) =>
    hud.error(String(error)),
  );
await useWorld(await (await fetch("/api/world")).json());
window.addEventListener("pagehide", () => {
  stream?.dispose();
  renderer.dispose();
});
// 验收入口仍使用实际会话、流式调度和绘制路径，不替换成测试场景。
globalThis.viewerR2 = {
  get state() {
    return {
      world,
      settings: { ...settings },
      camera: { ...camera, position: [...camera.position] },
      map: {
        open: map.open,
        camera: map.camera,
        center: [...map.center],
        overview: map.isOverview,
        selection: map.selection,
        pending: !!map.pending,
        tiles: map.tiles.length,
      },
      stream: stream?.stats,
      gpuTimes: renderer.times,
      frames: [...frames],
      error: renderer.gl.getError(),
    };
  },
  /** 固定相机朝向，供像素回归与截图使用。 @param {number[]} position @param {number[]} target */
  setCamera(position, target) {
    camera.position = [...position];
    const d = target.map((v, i) => v - position[i]);
    camera.yaw = Math.atan2(d[0], -d[2]);
    camera.pitch = Math.asin(d[1] / Math.hypot(...d));
    renderer.resetTiming();
  },
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
  get map() {
    return map;
  },
};
requestAnimationFrame(frame);
