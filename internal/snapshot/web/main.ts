import { createRenderer, ObjectRegistry, WorldSession, type Camera, type DataSource, type WorldSink } from "neomega-r2-core";
import { createStartup, createView, element, type ViewSettings } from "neomega-r2-frontend/ui";

interface Dimension {
  id: string;
  key: number;
  // 开场取景的方块包围盒，大存档取出生点附近有内容的区域。
  frame?: { min: number[]; max: number[] };
}
interface ExportedWorld {
  version: number;
  name: string;
  camera?: Camera;
  /** Viewer 导出时保存的显示设置；命令行导出的建筑使用播放器默认取景和画质。 */
  settings?: Partial<ViewSettings>;
  ocean?: number;
  dimensions: Dimension[];
}

function unpack(base64: string): Response {
  const binary = atob(base64), bytes = new Uint8Array(binary.length);
  for (let i = 0; i < bytes.length; i++) bytes[i] = binary.charCodeAt(i);
  const stream = new Blob([bytes]).stream().pipeThrough(new DecompressionStream("gzip"));
  return new Response(stream);
}

// 每次提交的 ArrayBuffer 都会转移给准备线程。重访区域时从 HTML 重新解压。
function staticSource(regions: Map<string, string>): DataSource {
  let sink: WorldSink | null = null;
  return {
    open(next) { sink = next; },
    want({ regions: wanted, objects }) {
      for (const { x, z } of wanted.add) {
        const data = regions.get(`${x}/${z}`);
        if (data === undefined) sink?.region(x, z, null);
        else unpack(data).arrayBuffer().then(
          buffer => sink?.region(x, z, buffer),
          error => sink?.error(x, z, error),
        );
      }
      for (const { x, z } of objects.add) sink?.objects(x, z, []);
    },
    close() { sink = null; },
  };
}

const startup = createStartup();

async function start() {
  const world: ExportedWorld = JSON.parse(element("r2-world").textContent);
  if (world.version !== 1 || !world.dimensions.length) throw Error("无法识别导出的世界数据");
  const resources: Record<string, string> = JSON.parse(element("r2-resources").textContent);
  const settings: ViewSettings = {
    tier: "basic", stillAccumulation: false, nearRadius: 1, farRadius: 32,
    speed: 32, resolution: "1", photoScale: 1, photoSupersample: 1,
    exportScale: 1, exportSupersample: 1, fog: true, time: 10, timeFlow: false,
    ...world.settings,
  };
  document.title = world.name + " · R2 建筑预览";
  const renderer = await createRenderer({
    tier: settings.tier,
    readResource: async name => {
      if (!(name in resources)) throw Error("网页缺少渲染资源 " + name);
      return unpack(resources[name]);
    },
    onProgress: ({ label }) => startup.stage(label),
  });
  const camera: Camera = { position: [0, 0, 0], yaw: -Math.PI / 4, pitch: -Math.PI / 6 };
  const near = element<HTMLInputElement>("near-radius");
  near.disabled = true;
  near.title = "建筑快照只包含静态方块";
  const view = createView({
    renderer, camera, settings, startup, scene: () => ({ name: world.name, ocean: world.ocean }),
    onStats: ({ stats, median }) => {
      if (status.checkVisibility()) status.textContent = `${stats.sections} 个子区块 · ${stats.loaded}/${stats.wanted} 个 r2region · GPU ${median?.toFixed(2) ?? "…"} ms`;
    },
  });
  const status = view.debugStatus;
  const sets = new Map<number, Map<string, string>>();
  for (const tag of document.querySelectorAll<HTMLScriptElement>("script[data-r2-region]")) {
    const key = Number(tag.dataset.dimension);
    if (!sets.has(key)) sets.set(key, new Map());
    sets.get(key)!.set(tag.dataset.r2Region!, tag.textContent);
  }

  let session: WorldSession | null = null;
  function openDimension(dimension: Dimension) {
    view.cancelPhoto();
    session?.dispose();
    if (world.camera) {
      camera.position = [...world.camera.position];
      camera.yaw = world.camera.yaw;
      camera.pitch = world.camera.pitch;
      camera.ortho = world.camera.ortho;
    } else if (dimension.frame) {
      const { min, max } = dimension.frame;
      const center = min.map((lo, i) => (lo + max[i]) / 2);
      const distance = Math.hypot(...max.map((hi, i) => hi - min[i])) * 1.1;
      camera.yaw = -Math.PI / 4;
      camera.pitch = -Math.PI / 6;
      camera.position = center.map((v, i) => v + distance * [Math.sqrt(3 / 8), 0.5, Math.sqrt(3 / 8)][i]);
    }
    session = new WorldSession(renderer, staticSource(sets.get(dimension.key)!), {
      registry: new ObjectRegistry(), onError: message => view.hud.error(message),
    });
    const chunks = element("r2-chunks-" + dimension.key).textContent;
    void view.setWorld(true, () => unpack(chunks).arrayBuffer()).catch(error => view.hud.error(String(error)));
  }
  const select = element<HTMLSelectElement>("export-dimension");
  const labels: Record<string, string> = { ".": "主世界", "DIM-1": "下界", DIM1: "末地", "0": "主世界", "1": "下界", "2": "末地" };
  for (const dimension of world.dimensions) select.add(new Option(labels[dimension.id] ?? dimension.id));
  select.hidden = select.length < 2;
  select.onchange = () => openDimension(world.dimensions[select.selectedIndex]);
  await startup.stage("正在准备建筑…");
  openDimension(world.dimensions[0]);
  function frame(now: number) {
    view.frame(now, session);
    requestAnimationFrame(frame);
  }
  requestAnimationFrame(frame);
  addEventListener("pagehide", event => {
    if (event.persisted) return;
    view.dispose();
    session?.dispose();
    renderer.dispose();
  });
}

start().catch((error) => startup.fail(error));
