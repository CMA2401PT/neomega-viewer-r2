// 应用组装存档会话、飞行/地图视图与同一射线帧循环；UI 事件和投影分别放在独立模块。
import { decodeTextures, createRenderer, ObjectRegistry, enchantingBook, WorldSession, r2linkSource } from "/r2/index.js";
import { FlightControls } from "./controls.js";
import { bindMovePad } from "./movepad.js";
import { createPicker } from "./picker.js";
import { createHUD, element, closePopovers } from "./hud.js";
import { drawAxes } from "./overlay.js";
import { FullscreenMap } from "./map.js";
import { report, describe, startDiagnostics } from "./diagnostics.js";
import { startupStage, finishStartup } from "./startup.js";

startDiagnostics();

const canvas = element("view");
canvas.addEventListener("webglcontextlost", () => report("context-lost"));
canvas.addEventListener("webglcontextrestored", () => report("context-restored"));
const camera = { position: [0, 80, 0], yaw: -0.7, pitch: -0.32 };
/** @type {import("./hud.js").ViewerSettings} */
const settings = {
  nearRadius: 2,
  farRadius: 32,
  resolution: "1.5",
  speed: 32,
  tier: "shading",
  time: 10,
  timeFlow: true,
  fog: true,
  stillAccumulation: false,
  photoScale: 2,
  texturePath: "",
  textureSize: 128,
  ...JSON.parse(localStorage.getItem("neomega-r2-settings") || "{}"),
};
// 旧版按样本数设置累积与拍照的上限，现在只有开关，不再有上限。
delete settings.stillSamples;
delete settings.photoSamples;
// 旧版的阴影开关由预览渲染器的档位取代：光影渲染器总有阴影，低档没有。
delete settings.shadows;
/** 各档渲染器的名字；前三档用于预览，拍照与静止累积总是使用累积渲染器。 */
const TIER_NAMES = { voxel: "体素渲染器", basic: "基础渲染器", shading: "光影渲染器", accumulation: "累积渲染器" };
if (!["voxel", "basic", "shading"].includes(settings.tier)) settings.tier = "shading";
// 导出画面是屏幕实际像素的 2 倍边长，每个像素 4×4 超采样；超过这个像素数时等比缩小，避免显存不足。
const EXPORT_SCALE = 2,
  EXPORT_SUPERSAMPLE = 4,
  EXPORT_PIXELS = 24e6;
const controls = new FlightControls(canvas, camera);
bindMovePad(element("movepad"), controls);
const registry = new ObjectRegistry().register("enchanting_book", () =>
  enchantingBook("/r2/assets/book.png"),
);
let renderer,
  map,
  /** 当前世界的会话与数据源。 @type {WorldSession|null} */
  session = null,
  /** @type {import("/r2/index.js").R2LinkSource|null} */
  source = null,
  // 正在换成外部数据源：这期间本机世界的断开是预期的，不跟随服务端。
  connecting = false,
  // 最近一帧的视图（主相机或地图俯视），拍照与导出都拍这个画面。
  currentView = null,
  // 画面实际使用的预览档；选中的档还在编译时沿用上一档。
  shownTier = settings.tier,
  world = { loaded: false },
  last = performance.now(),
  statusAt = 0;
let frames = [],
  drawn = "",
  drawnAt = 0,
  continuous = false,
  photo = null,
  // 等累积渲染器编译好再开始的拍照请求。
  photoWaiting = false;
const hud = createHUD({
  settings,
  jump: (position) => {
    camera.position = position;
  },
  changed: saveSettings,
});
// 界面上弹出的错误同时写进诊断日志。
const showError = hud.error;
hud.error = (message) => {
  report("hud-error", { message: String(message).slice(0, 4000) });
  showError(message);
};

/** 设置持久化不参与逐帧时间推进，避免连续写入浏览器存储。 */
function saveSettings() {
  // 启动时这里先于渲染器创建运行；需要的档等渲染器就绪后再编译。
  if (renderer) {
    compileTier(settings.tier);
    if (settings.stillAccumulation) compileTier("accumulation");
  }
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
/** 所有世界都经 R2Link 读取：本机存档与内置场景连接同源的 /r2link，外部数据源连接用户给的地址。
 * 内置场景额外提供开场取景与时间；外部数据源带 connection 与握手给出的相机。
 * @param {{loaded:boolean,id:number,name:string,path?:string,position:number[],scene?:{ocean:number,camera:{position:number[],yaw:number,pitch:number},time:number},camera?:{position:number[],yaw?:number,pitch?:number},connection?:{url:string,state:string,message:string}}} info
 */
let worldQueue = Promise.resolve();
/** 打开、关闭、连接与启动共用切换队列；编号不比当前世界新的切换被忽略。
 * @param {Object} info @param {import("/r2/index.js").R2LinkSource} [linked] 外部数据源
 */
function useWorld(info, linked) {
  const job = worldQueue.then(() => installWorld(info, linked));
  worldQueue = job.catch(() => {});
  return job;
}
/** 本机世界的数据源。服务端切换或关闭世界时断开旧连接，这时改用服务端的当前世界（可能由别的页面打开）。
 * @param {{id:number}} info
 */
function localSource(info) {
  const url = new URL(`/r2link?world=${info.id}`, location.href);
  url.protocol = url.protocol === "https:" ? "wss:" : "ws:";
  const own = r2linkSource(url.href, {
    retry: 0,
    onState: (state, message) => {
      // 自己关闭时没有 message。
      if (state === "closed" && message && source === own && !connecting) followServer(message);
    },
  });
  return own;
}
/** @param {string} message */
async function followServer(message) {
  try {
    const info = await (await fetch("/api/world")).json();
    if (info.id > world.id) return await useWorld(info);
  } catch {}
  hud.error(`世界连接已断开：${message}`);
}
/** 换世界时结束拍照，按新世界的开场位置摆放相机。 */
async function installWorld(info, linked) {
  if (world.id === info.id) { world = info; return; }
  if (world.id > info.id) return linked?.close();
  await cancelPhoto();
  // 离开开场场景或直接载入外部来源时从白天开始；来源之间切换保留当前时间。
  const startDaytime = !info.scene && (!world.loaded || !!world.scene);
  const old = session;
  session = source = null;
  old?.dispose();
  world = info;
  closePopovers();
  hud.world(info);
  hud.connection(info.connection);
  const current = (source = linked || localSource(info));
  map.setWorld(info, current).catch((error) => {
    if (source === current && current.state === "connected") hud.error(String(error));
  });
  const start = info.scene?.camera || info.camera;
  camera.position = [...(start?.position || info.position)];
  camera.yaw = start?.yaw ?? -0.7;
  camera.pitch = start?.pitch ?? -0.32;
  if (info.scene || startDaytime) {
    settings.time = info.scene?.time ?? 10;
    hud.settings();
  }
  session = new WorldSession(renderer, current, {
    registry,
    onError: (message) => hud.error(message),
  });
  session.focus(camera.position, settings);
  frames = [];
  drawn = "";
}
/** 目录选择器与验收入口共用同一存档打开操作。
 * @param {string} path
 * @param {{dimension?:string,bedrockFix?:boolean,structureMemoryMiB?:number,structureWorkers?:number}} options
 * @param {(value:Object)=>void} [progress]
 * @param {AbortSignal} [signal]
 */
async function openWorld(path, options = {}, progress, signal) {
  const response = await fetch("/api/world/open", {
    method: "POST",
    headers: { "Content-Type": "application/json", "Accept": "application/x-ndjson" },
    body: JSON.stringify({ path, ...options }),
    signal,
  });
  if (!response.ok) throw Error(await response.text());
  const reader = response.body.pipeThrough(new TextDecoderStream()).getReader();
  let pending = "";
  while (true) {
    const { value, done } = await reader.read();
    if (done) break;
    pending += value;
    let newline;
    while ((newline = pending.indexOf("\n")) >= 0) {
      const message = JSON.parse(pending.slice(0, newline));
      pending = pending.slice(newline + 1);
      if (message.type === "error") throw Error(message.message);
      if (message.type === "progress") progress?.(message.progress);
      if (message.type === "world") await useWorld(message.world);
    }
  }
}
saveSettings();
const compileStarted = performance.now();
const showCompile = async ({ index, total, label }) => {
  report("compile", { index, total, label, seconds: +((performance.now() - compileStarted) / 1000).toFixed(1) });
  await startupStage(`${label}（${index}/${total}）`);
};
await startupStage("正在初始化图形设备…");
try {
  renderer = await createRenderer(canvas, { tier: settings.tier, onProgress: showCompile });
  report("renderer-ready", { seconds: +((performance.now() - compileStarted) / 1000).toFixed(1) });
} catch (error) {
  report("renderer-failed", { seconds: +((performance.now() - compileStarted) / 1000).toFixed(1), message: describe(error) });
  throw error;
}
await startupStage("正在准备场景…");
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
  picker({ world });
};
element("world-dimension").onchange = async (event) => {
  const select = event.target;
  select.disabled = true;
  try {
    await openWorld(world.path, {
      dimension: select.value,
      bedrockFix: world.bedrockFix,
      structureMemoryMiB: world.structureMemoryMiB,
      structureWorkers: world.structureWorkers,
    });
  } catch (error) { select.value = world.dimension; hud.error(String(error)); }
  finally { select.disabled = false; }
};
/** 先连上外部数据源，握手成功后才关闭当前存档并换成它；连不上时当前世界不受影响。
 * @param {string} url ws:// 或 wss:// 地址
 */
async function connectSource(url) {
  const connection = { url, state: "connecting", message: "" };
  /** @type {import("/r2/index.js").R2LinkSource|undefined} */
  let linked;
  try {
    linked = r2linkSource(url, {
      onState: (state, message) => {
        connection.state = state;
        connection.message = message || "";
        if (source === linked) hud.connection(connection);
      },
      onScene: (scene) => {
        if (source !== linked) return;
        world.name = scene.name;
        hud.world(world);
        hud.connection(connection);
        map.refreshChunks().catch(() => {});
      },
      onCamera: (next) => {
        if (source !== linked) return;
        camera.position = [...next.position];
        if (next.yaw != null) camera.yaw = next.yaw;
        if (next.pitch != null) camera.pitch = next.pitch;
      },
    });
    const { scene, camera: start } = await linked.welcome;
    connecting = true;
    const response = await fetch("/api/world/close", { method: "POST" });
    if (!response.ok) throw Error(await response.text());
    const closed = await response.json();
    localStorage.setItem("neomega-r2-link", url);
    // 编号排在关闭得到的 Möbius 之后、下一次打开之前，晚到的旧世界切换因此被忽略。
    await useWorld({
      loaded: true,
      id: closed.id + 0.5,
      name: scene.name,
      kind: "R2Link",
      position: start?.position || [0, 80, 0],
      camera: start,
      connection,
    }, linked);
  } catch (error) {
    linked?.close();
    throw error;
  } finally {
    connecting = false;
  }
}
element("connect-source").onclick = () => {
  closePopovers();
  element("link-url").value =
    world.connection?.url || localStorage.getItem("neomega-r2-link") || "ws://127.0.0.1:2414/r2link";
  element("link-message").textContent = "";
  element("link-dialog").showModal();
};
element("link-start").onclick = async () => {
  const button = element("link-start");
  button.disabled = true;
  element("link-message").textContent = "正在连接…";
  try {
    await connectSource(element("link-url").value.trim());
    element("link-dialog").close();
  } catch (error) {
    element("link-message").textContent = String(error.message || error);
  } finally {
    button.disabled = false;
  }
};
// 关闭存档或断开外部数据源，都回到 Möbius。
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
    if (session) await session.setTextures(set);
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

// 预览之外的各档启动时不编译；第一次需要时在后台编译，期间画面继续使用已有的档。
const taskStatus = element("task-status");
/** 正在编译的档与开始时间。 @type {Map<string,{promise:Promise<void>,since:number}>} */
const compiling = new Map();
/** 编译一档渲染器；已在编译或已经可用时不重复。失败时提示，之后再次需要时重试。
 * 提示先写进页面：渲染器会等它显示出来再开始编译，Windows 上编译期间页面可能停止刷新。
 * @param {"voxel"|"basic"|"shading"|"accumulation"} tier
 */
function compileTier(tier) {
  if (renderer.ready(tier)) return Promise.resolve();
  const existing = compiling.get(tier);
  if (existing) return existing.promise;
  const since = performance.now(),
    seconds = () => +((performance.now() - since) / 1000).toFixed(1);
  report("compile-start", { tier });
  const promise = renderer
    .compile(tier)
    .then(() => report("compile-ready", { tier, seconds: seconds() }))
    .catch((error) => {
      report("compile-failed", { tier, seconds: seconds(), message: describe(error) });
      if (tier === "accumulation") {
        photoWaiting = false;
        setPhotoButton();
      }
      hud.error(`${TIER_NAMES[tier]}准备失败：${error.message || error}`);
      throw error;
    })
    .finally(() => {
      compiling.delete(tier);
      drawn = "";
      showTaskStatus(performance.now());
    });
  promise.catch(() => {});
  compiling.set(tier, { promise, since });
  showTaskStatus(since);
  return promise;
}
/** 底部提示：渲染器编译进度，或拍照、导出时的操作说明。 */
function showTaskStatus(now) {
  const preview = compiling.get(settings.tier),
    accumulation = compiling.get("accumulation"),
    elapsed = (job) => `已用 ${Math.floor((now - job.since) / 1000)} 秒`;
  const text = photo
    ? photo.kind === "export"
      ? `正在导出 · ${photo.samples}/${EXPORT_SUPERSAMPLE ** 2} 轮超采样，完成后自动保存 · 再按导出按钮或 Esc 取消`
      : `拍照中 · 已累积 ${photo.samples} 样本，画面会越来越干净 · 满意时再按相机按钮保存，按 Esc 放弃`
    : preview
      ? `正在编译${TIER_NAMES[settings.tier]}，期间画面可能停顿 · ${elapsed(preview)}${shownTier !== settings.tier ? ` · 完成前继续使用${TIER_NAMES[shownTier]}` : ""}`
      : accumulation && (settings.stillAccumulation || photoWaiting)
        ? `正在编译累积渲染器，比较耗时，需要 1～2 分钟，期间画面可能停顿 · ${elapsed(accumulation)}${photoWaiting ? " · 准备好后自动开始拍照，再按相机按钮取消" : ""}`
        : "";
  if (taskStatus.textContent !== text) taskStatus.textContent = text;
  taskStatus.hidden = !text;
  for (const button of document.querySelectorAll(".tier"))
    button.setAttribute("aria-busy", String(compiling.has(button.dataset.tier)));
}
/** 拍照与导出两个按钮的状态：进行中的一项按下，另一项停用。 */
function setPhotoButton() {
  const kind = photo?.kind ?? (photoWaiting ? "photo" : null);
  element("photo").setAttribute("aria-pressed", String(kind === "photo"));
  element("photo").title = element("photo").ariaLabel =
    photo?.kind === "photo" ? "保存照片" : photoWaiting ? "取消拍照" : "拍照（累积渲染器）";
  element("photo").disabled = kind === "export";
  element("export").setAttribute("aria-pressed", String(kind === "export"));
  element("export").title = element("export").ariaLabel =
    kind === "export" ? "取消导出" : "导出当前画面：用当前预览渲染器以 2 倍尺寸、4×4 超采样重新渲染";
  element("export").disabled = kind === "photo";
  element("photo-progress").hidden = !photo;
}
// 拍照使用独立缓冲，不把 HUD 或标记写入照片。按下后画面上实时显示照片逐渐收敛，再按一次保存。
element("photo").onclick = () => {
  if (photo) return finishPhoto(true);
  if (photoWaiting) {
    photoWaiting = false;
    return setPhotoButton();
  }
  if (renderer.ready("accumulation")) return startPhoto("photo");
  photoWaiting = true;
  setPhotoButton();
  compileTier("accumulation").then(() => {
    if (photoWaiting) startPhoto("photo");
  }, () => {});
};
// 导出用当前预览档按固定网格超采样重画当前画面，取满后自动保存；进行中再按一次取消。
element("export").onclick = () => {
  if (photo) return cancelPhoto();
  startPhoto("export");
};
addEventListener("keydown", (event) => {
  if (event.key === "Escape" && (photo || photoWaiting)) cancelPhoto();
});
/** 拍照总是用累积渲染器，尺寸为渲染画面乘照片倍数；导出用当前预览档，尺寸为屏幕实际像素的 2 倍。
 * 两者都拍最近一帧的视图，地图打开时就是俯视画面。
 * @param {"photo"|"export"} kind
 */
function startPhoto(kind) {
  photoWaiting = false;
  if (!currentView) return;
  let width, height;
  if (kind === "photo") {
    const scale = renderScale() * settings.photoScale;
    width = innerWidth * scale;
    height = innerHeight * scale;
  } else {
    const gl = renderer.gl,
      limit = Math.min(gl.getParameter(gl.MAX_TEXTURE_SIZE), gl.getParameter(gl.MAX_RENDERBUFFER_SIZE));
    width = innerWidth * devicePixelRatio * EXPORT_SCALE;
    height = innerHeight * devicePixelRatio * EXPORT_SCALE;
    const shrink = Math.min(1, limit / Math.max(width, height), Math.sqrt(EXPORT_PIXELS / (width * height)));
    width *= shrink;
    height *= shrink;
  }
  photo = renderer.photograph(currentView.camera, {
    width: Math.round(width),
    height: Math.round(height),
    ...(kind === "export" ? { tier: shownTier, supersample: EXPORT_SUPERSAMPLE } : {}),
    time: performance.now() / 1000,
    sun: sunDirection(settings.time),
    ...(world.scene ? { ocean: world.scene.ocean } : {}),
    fog: currentView.fog,
    objects: currentView.objects,
  });
  photo.kind = kind;
  photo.name = `${world.name || "天空"}-${new Date().toTimeString().slice(0, 8).replaceAll(":", "")}${kind === "export" ? `-${shownTier}` : ""}.png`;
  setPhotoButton();
  showTaskStatus(performance.now());
}
/** 放弃正在进行或正在等待的拍照。 */
function cancelPhoto() {
  photoWaiting = false;
  if (photo) return finishPhoto(false);
  setPhotoButton();
}
/** 保存时下载，放弃时直接释放照片缓冲；编码期间继续冻结场景。
 * @param {boolean} download
 */
async function finishPhoto(download) {
  const job = photo;
  if (!job || job.finishing) return;
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
  setPhotoButton();
  showTaskStatus(performance.now());
}

/** 同一帧循环消费当前视图的相机；世界、光照和累积仍使用同一条绘制路径。
 * @param {number} now
 */
function frame(now) {
  requestAnimationFrame(frame);
  const dt = Math.min(0.05, (now - last) / 1000);
  last = now;
  if (now - statusAt > 100) showTaskStatus(now);
  if (photo) {
    if (photo.finishing) return;
    photo.step();
    photo.present();
    element("photo-progress").textContent = photo.kind === "export"
      ? `${photo.samples}/${EXPORT_SUPERSAMPLE ** 2}`
      : `${photo.samples} 样本`;
    if (photo.complete) finishPhoto(true);
    if (now - statusAt > 100) {
      statusAt = now;
      element("fps").textContent = photo.kind === "export" ? "导出中" : "拍照中";
    }
    return;
  }
  const view = map.frame(settings);
  currentView = view;
  if (renderer.ready(settings.tier)) shownTier = settings.tier;
  controls.enabled = view.objects;
  controls.update(dt, Number(settings.speed));
  if (settings.timeFlow) {
    settings.time = (settings.time + (dt * 24) / 240) % 24;
    hud.settings();
  }
  session?.focus(view.center, { ...settings, nearPosition: camera.position });
  session?.publish();
  map.refresh(session);
  const scale = renderScale(),
    width = Math.round(innerWidth * scale),
    height = Math.round(innerHeight * scale);
  const sun = sunDirection(settings.time),
    objects = view.objects;
  const state = JSON.stringify([
    view,
    width,
    height,
    shownTier,
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
  // 静止时一直累积，直到画面再次变化；没有样本上限。
  const accumulating = settings.stillAccumulation && renderer.ready("accumulation");
  if (!view.overview && (changed || accumulating)) {
    rendered = renderer.draw(view.camera, {
      width,
      height,
      tier: shownTier,
      accumulate: !changed,
      time: now / 1000,
      sun,
      ...(world.scene ? { ocean: world.scene.ocean } : {}),
      fog: view.fog,
      objects,
    });
    if (rendered && session?.stats.loaded > 0) finishStartup();
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
        ? `累积 ${renderer.accumulated}`
        : "静止"
      : `${Math.round(1000 / interval) || "—"} FPS`;
    if (session) {
      const stats = session.stats,
        times = renderer.times.sort((a, b) => a - b),
        median = times[Math.floor(times.length / 2)];
      hud.loading(stats, world.connection);
      const clipped = source.clippedSections;
      element("height-diagnostics").hidden = clipped === 0;
      element("height-diagnostics").textContent = `有 ${clipped} 个 section 超出可显示高度（Y -64～447，按已收到的区域统计）`;
      hud.details([
        ["来源", world.kind || "内置场景"],
        ["预览渲染器", TIER_NAMES[shownTier]],
        ...(world.reusedStructure ? [["结构输出", "复用已有存档"]] : []),
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
        ...(stats.unknownBlocks ? [["未知方块", String(stats.unknownBlocks)]] : []),
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

if (settings.texturePath || settings.textureSize !== 128) {
  await startupStage("正在加载自定义材质…");
  await applyTextures(settings.texturePath).catch((error) =>
    hud.error(String(error)),
  );
}
await startupStage("正在读取场景信息…");
await useWorld(await (await fetch("/api/world")).json());
await startupStage("正在加载场景，准备第一帧…");
// 运行一阵后记下实际帧率与 GPU 时间，判断这种浏览器与图形后端组合是否够快。
for (const delay of [15000, 60000])
  setTimeout(() => {
    const times = renderer.times.sort((a, b) => a - b),
      interval = frames.reduce((a, b) => a + b, 0) / frames.length;
    report("performance", {
      canvas: [canvas.width, canvas.height],
      resolution: settings.resolution,
      gpuMedianMs: times[times.length >> 1] ?? null,
      fps: interval ? Math.round(1000 / interval) : null,
      world: world.kind || "mobius",
    });
  }, delay);
if (settings.stillAccumulation) compileTier("accumulation");
window.addEventListener("pagehide", () => {
  session?.dispose();
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
      stream: session?.stats,
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
  connectSource,
  applyTextures,
  close: () => element("close-world").click(),
  get renderer() {
    return renderer;
  },
  get session() {
    return session;
  },
  get source() {
    return source;
  },
  get map() {
    return map;
  },
};
requestAnimationFrame(frame);
