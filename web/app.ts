// 应用组装存档会话、飞行/地图视图与同一射线帧循环；UI 事件和投影分别放在独立模块。
import { createRenderer, ObjectRegistry, enchantingBook, WorldSession, r2linkSource } from "/r2/index.js";
import { loadMaterial, saveMaterial } from "./material-preference.js";
import { materialControls } from "/r2/common_ui/material-controls.js";
import { exportSnapshot } from "./export.js";
import type { TextureSet, Renderer, RendererOptions, R2LinkSource } from "/r2/index.js";
import type { Connection, OpenOptions, Progress, ViewerWorld } from "./types.js";
import { createView, type ViewSettings, type ViewStats } from "/r2/common_ui/view.js";
import { element, closePopovers } from "/r2/common_ui/hud.js";
import { showWorldInfo, showConnection } from "./world-info.js";
import { createPicker } from "./picker.js";
import { startupStage } from "/r2/common_ui/startup.js";


const canvas = element<HTMLCanvasElement>("view");
const camera = { position: [0, 80, 0], yaw: -0.7, pitch: -0.32 };
const savedSettings = { ...JSON.parse(localStorage.getItem("neomega-r2-settings") || "{}") };
// 清除旧版的样本上限和阴影开关。
for (const key of ["stillSamples", "photoSamples", "shadows", "texturePath"]) delete savedSettings[key];
const settings: ViewSettings & { textureSize: number } = {
  nearRadius: 2,
  farRadius: 32,
  resolution: "native",
  speed: 32,
  tier: "shading",
  time: 10,
  timeFlow: true,
  fog: true,
  stillAccumulation: false,
  photoScale: 2, photoSupersample: 1,
  exportScale: 2, exportSupersample: 1,
  textureSize: 128,
  ...savedSettings,
};
if (!["voxel", "basic", "shading"].includes(settings.tier)) settings.tier = "shading";
const registry = new ObjectRegistry().register("enchanting_book", () =>
  enchantingBook("/r2/assets/book.png"),
);
let renderer: Renderer;
let activeTextures: TextureSet | undefined;
let exporting: AbortController | null = null;
const exportButton = element<HTMLButtonElement>("export-html");
const exportStatus = element("export-status");
let session: WorldSession | null = null;
let source: R2LinkSource | null = null;
// 外部来源握手期间不跟随本机世界的断开。
let connecting = false;
let world: ViewerWorld = { loaded: false, id: -1, name: "", position: camera.position };
let continuous = false;
/** 所有世界都经 R2Link 读取：本机存档与内置场景连接同源的 /r2link，外部数据源连接用户给的地址。
 * 内置场景额外提供开场取景与时间；外部数据源带 connection 与握手给出的相机。 */
let worldQueue = Promise.resolve();
/** 打开、关闭、连接与启动共用切换队列；编号不比当前世界新的切换被忽略。 */
function useWorld(info: ViewerWorld, linked?: R2LinkSource) {
  const job = worldQueue.then(() => installWorld(info, linked));
  worldQueue = job.catch(() => {});
  return job;
}
/** 本机世界的数据源。服务端切换或关闭世界时断开旧连接，这时改用服务端的当前世界（可能由别的页面打开）。 */
function localSource(info: ViewerWorld) {
  const url = new URL(`/r2link?world=${info.id}`, localServiceURL);
  url.protocol = url.protocol === "https:" ? "wss:" : "ws:";
  const own = r2linkSource(url.href, {
    retry: 0,
    onState: (state, message) => {
      // 自己关闭时没有 message。
      if (state === "closed" && message && source === own && !connecting) followServer(own, message);
    },
  });
  return own;
}
async function followServer(closed: R2LinkSource, message: string) {
  try {
    const info = await (await fetch("/api/world")).json();
    if (source !== closed || connecting) return;
    if (info.id > world.id) return await useWorld(info);
  } catch {}
  if (source === closed && !connecting) hud.error(`世界连接已断开：${message}`);
}
/** 换世界时结束拍照，按新世界的开场位置摆放相机。 */
async function installWorld(info: ViewerWorld, linked?: R2LinkSource) {
  if (world.id === info.id) { world = info; return; }
  if (world.id > info.id) return linked?.close();
  exporting?.abort(new Error("场景已切换"));
  await view.cancelPhoto();
  // 离开开场场景或直接载入外部来源时从白天开始；来源之间切换保留当前时间。
  const startDaytime = !info.scene && (!world.loaded || !!world.scene);
  const old = session;
  session = source = null;
  old?.dispose();
  world = info;
  closePopovers();
  showWorldInfo(info);
  showConnection(info.connection);
  const current = (source = linked || localSource(info));
  view.setWorld(info.loaded, async () => {
    await current.welcome;
    return current.chunks();
  }).catch((error) => {
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
  exportButton.disabled = !!exporting;
}
/** 目录选择器与验收入口共用同一存档打开操作。 */
async function openWorld(path: string, options: OpenOptions = {}, progress?: (value: Progress) => void, signal?: AbortSignal) {
  const response = await fetch("/api/world/open", {
    method: "POST",
    headers: { "Content-Type": "application/json", "Accept": "application/x-ndjson" },
    body: JSON.stringify({ path, ...options }),
    signal,
  });
  if (!response.ok) throw Error(await response.text());
  if (!response.body) throw Error("服务端未返回打开进度");
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
const showCompile: NonNullable<RendererOptions["onProgress"]> = ({ index, total, label }) =>
  startupStage(`${label}（${index}/${total}）`);
await startupStage("正在初始化图形设备…");
renderer = await createRenderer(canvas, { tier: settings.tier, onProgress: showCompile });
await startupStage("正在准备场景…");
const view = createView({
  renderer, camera, settings,
  scene: () => ({ name: world.name, ocean: world.scene?.ocean, connection: world.connection }),
  onSettings: () => localStorage.setItem("neomega-r2-settings", JSON.stringify(settings)),
  onStats: updateViewerStatus,
});
const { hud, map, frames } = view;
exportButton.onclick = async () => {
  if (!source || exporting) return;
  exporting = new AbortController();
  exportButton.disabled = true;
  exportStatus.hidden = false;
  exportStatus.textContent = "正在导出…";
  const name = world.name;
  try {
    // 点击时拍下整个视图；材质已经内嵌，播放器无需本机的文件路径。
    const { textureSize, ...viewSettings } = settings;
    const file = await exportSnapshot({
      sourceURL: source.url, name, dimension: world.dimension ?? ".", ocean: world.scene?.ocean,
      camera: structuredClone(camera), settings: structuredClone(viewSettings),
      states: renderer.states, textures: activeTextures,
    }, exporting.signal, count => { exportStatus.textContent = `已导出 ${count} 个区域`; });
    const url = URL.createObjectURL(file);
    const link = document.createElement("a");
    link.href = url;
    link.download = name.replace(/[\\/:*?"<>|]/g, "_") + ".html";
    link.click();
    setTimeout(() => URL.revokeObjectURL(url), 60000);
  } catch (error) {
    if (!exporting.signal.aborted) hud.error(String(error));
  } finally {
    exporting = null;
    exportButton.disabled = !source;
    exportStatus.hidden = true;
  }
};

const picker = createPicker(openWorld);
element<HTMLButtonElement>("open-world").onclick = () => {
  closePopovers();
  picker({ world });
};
const select = element<HTMLSelectElement>("world-dimension");
select.onchange = async () => {
  if (!world.path) return;
  select.disabled = true;
  try {
    await openWorld(world.path, {
      dimension: select.value,
      bedrockFix: world.bedrockFix,
      structureMemoryMiB: world.structureMemoryMiB,
      structureWorkers: world.structureWorkers,
      structureRoot: world.structureRoot,
    });
  } catch (error) { select.value = world.dimension ?? ""; hud.error(String(error)); }
  finally { select.disabled = false; }
};
/** 先连上外部数据源，握手成功后才关闭当前存档并换成它；连不上时当前世界不受影响。 */
async function connectSource(url: string) {
  const connection: Connection = { url, state: "connecting", message: "" };
  let linked: R2LinkSource | undefined;
  try {
    linked = r2linkSource(url, {
      onState: (state, message) => {
        connection.state = state;
        connection.message = message || "";
        if (source === linked) showConnection(connection);
      },
      onScene: (scene) => {
        if (source !== linked) return;
        world.name = scene.name;
        showWorldInfo(world);
        showConnection(connection);
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
element<HTMLButtonElement>("connect-source").onclick = () => {
  closePopovers();
  element<HTMLInputElement>("link-url").value =
    world.connection?.url || localStorage.getItem("neomega-r2-link") || "ws://127.0.0.1:2414/r2link";
  element<HTMLOutputElement>("link-message").textContent = "";
  element<HTMLDialogElement>("link-dialog").showModal();
};
element<HTMLButtonElement>("link-start").onclick = async () => {
  const button = element<HTMLButtonElement>("link-start");
  button.disabled = true;
  element<HTMLOutputElement>("link-message").textContent = "正在连接…";
  try {
    await connectSource(element<HTMLInputElement>("link-url").value.trim());
    element<HTMLDialogElement>("link-dialog").close();
  } catch (error) {
    element<HTMLOutputElement>("link-message").textContent = error instanceof Error ? error.message : String(error);
  } finally {
    button.disabled = false;
  }
};
// 关闭存档或断开外部数据源，都回到 Möbius。
element<HTMLButtonElement>("close-world").onclick = async () => {
  const response = await fetch("/api/world/close", { method: "POST" });
  if (!response.ok) return hud.error(await response.text());
  await useWorld(await response.json());
};
// 文件留在浏览器内。导出 HTML 使用已经安装的同一套模型和贴图。
let savedMaterial = await loadMaterial().catch(() => null);
const materials = materialControls(element("material-controls"), async (set, input, limit) => {
  const applied = worldQueue.then(async () => {
    if (session) await session.setTextures(set);
    else renderer.setTextures(set);
    activeTextures = set;
  });
  worldQueue = applied.catch(() => {});
  await applied;
  settings.textureSize = limit;
  if (input !== savedMaterial) {
    await saveMaterial(input).catch(error => hud.error("材质已应用，但无法记住选择：" + error));
    savedMaterial = input;
  }
  view.refreshSettings();
}, settings.textureSize);

function updateViewerStatus({ stats, median }: ViewStats) {
  if (view.debugStatus.checkVisibility()) {
    view.debugStatus.textContent = `已加载 ${stats.loaded}/${stats.wanted} 个区域 · GPU 中位耗时 ${median?.toFixed(2) ?? "…"} ms`;
  }
  const clipped = source?.clippedSections ?? 0;
  element("height-diagnostics").hidden = clipped === 0;
  element("height-diagnostics").textContent = `有 ${clipped} 个子区块（subchunk）超出可显示高度（Y -64～447，按已收到的 r2region 统计）`;
}

function frame(now: number) {
  requestAnimationFrame(frame);
  view.frame(now, session, { continuous });
}

if (savedMaterial || settings.textureSize !== 128) {
  await materials.apply(savedMaterial, settings.textureSize).catch(error => hud.error(String(error)));
}
await startupStage("正在读取场景信息…");
// Electron 页面使用固定来源，WebSocket 直连宿主本次启动的回环端口。
const localServiceURL = location.protocol === "r2:"
  ? await (await fetch("/desktop/service-url")).json()
  : location.href;
await useWorld(await (await fetch("/api/world")).json());
await startupStage("正在加载场景，准备第一帧…");
window.addEventListener("pagehide", () => {
  materials.dispose();
  view.dispose();
  session?.dispose();
  renderer.dispose();
});
// 验收入口仍使用实际会话、流式调度和绘制路径，不替换成测试场景。
const viewerR2 = {
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
    };
  },
  /** 固定相机朝向，供像素回归与截图使用。 */
  setCamera(position: number[], target: number[]) {
    camera.position = [...position];
    const d = target.map((v, i) => v - position[i]);
    camera.yaw = Math.atan2(d[0], -d[2]);
    camera.pitch = Math.asin(d[1] / Math.hypot(...d));
    renderer.resetTiming();
  },
  set continuous(value: boolean) {
    continuous = value;
  },
  openWorld,
  connectSource,
  applyTextures: materials.apply,
  close: () => element<HTMLButtonElement>("close-world").click(),
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
Object.assign(globalThis, { viewerR2 });
requestAnimationFrame(frame);
