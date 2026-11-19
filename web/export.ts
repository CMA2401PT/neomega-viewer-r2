import { decodeRegion, r2linkSource, type Camera, type RegionCoord, type TextureSet } from "/r2/index.js";
import type { ViewSettings } from "/r2/common_ui/view.js";

interface Snapshot {
  sourceURL: string;
  name: string;
  dimension: string;
  camera: Camera;
  settings: ViewSettings;
  ocean?: number;
  states: Uint32Array;
  textures?: TextureSet;
}

// 与离线播放器共用逐区域 gzip 格式。Blob 只保留压缩后的区域，原始数据在每批结束后释放。
async function compressed(data: ArrayBuffer): Promise<string> {
  const stream = new Blob([data]).stream().pipeThrough(new CompressionStream("gzip"));
  const blob = await new Response(stream).blob();
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onerror = () => reject(reader.error);
    reader.onload = () => resolve(String(reader.result).split(",")[1]);
    reader.readAsDataURL(blob);
  });
}

function jsonScript(value: unknown) {
  return JSON.stringify(value).replaceAll("<", "\\u003c");
}

async function player(signal: AbortSignal, textures?: TextureSet) {
  const response = await fetch("/api/export/template", { signal });
  if (!response.ok) throw Error(await response.text());
  let html = await response.text();
  if (textures) {
    const tag = /(<script type="application\/json" id="r2-resources">)([^<]+)<\/script>/;
    const match = tag.exec(html);
    if (!match) throw Error("离线播放器缺少资源表");
    const resources = JSON.parse(match[2]);
    for (const [name, data] of textures.files) {
      signal.throwIfAborted();
      resources[name] = await compressed(data);
    }
    html = html.replace(tag, () => match[1] + jsonScript(resources) + "</script>");
  }
  return html.split("<!-- R2_WORLD -->");
}

// 一张 32×32 区块位图覆盖 4×4 个 r2region。仅请求有内容的区域，包含视野外的建筑。
function regionsInTile(tile: DataView): RegionCoord[] {
  const regions = new Set<number>();
  for (let i = 0; i < 1024; i++) {
    if (tile.getUint8(8 + (i >> 3)) & (1 << (i & 7)))
      regions.add(((i & 31) >> 3) + 4 * (i >> 8));
  }
  return [...regions].map(i => ({
    x: tile.getInt32(0, true) * 4 + (i & 3),
    z: tile.getInt32(4, true) * 4 + (i >> 2),
  }));
}

export async function exportSnapshot(input: Snapshot, signal: AbortSignal, progress: (regions: number) => void) {
  const [before, after] = await player(signal, input.textures);
  signal.throwIfAborted();
  const parts: BlobPart[] = [before];
  const maps: BlobPart[] = [];
  let written = 0;
  const requests = new Map<string, (data: ArrayBuffer | null) => void>();
  // 独立订阅避免导出期间移动相机改变请求集合。来源断开或切换时舍弃未完成的文件。
  let fail!: (error: unknown) => void;
  const stopped = new Promise<never>((_, reject) => { fail = reject; });
  const abort = () => fail(signal.reason);
  const source = r2linkSource(input.sourceURL, {
    retry: 0,
    onState: (state, reason) => { if (state === "closed") fail(Error(reason || "数据源已关闭")); },
    onScene: () => fail(Error("导出期间数据源切换了场景")),
  });
  signal.addEventListener("abort", abort, { once: true });
  source.open({
    region: (x, z, data) => requests.get(x + "/" + z)?.(data),
    error: (_x, _z, error) => fail(error),
    objects() {},
  });
  const want = (add: RegionCoord[], remove: RegionCoord[]) => source.want({
    center: [input.camera.position[0], input.camera.position[2]],
    regions: { add, remove },
    objects: { add: [], remove: [] },
  });
  try {
    await Promise.race([stopped, source.welcome]);
    const index = await Promise.race([stopped, source.chunks()]);
    if (index.byteLength % 136) throw Error("数据源返回的区块占用表不完整");
    for (let offset = 0; offset < index.byteLength; offset += 136) {
      const tile = new DataView(index, offset, 136);
      const regions = regionsInTile(tile);
      const visible = new Uint8Array(136);
      visible.set(new Uint8Array(index, offset, 8));
      const batch = regions.map(({ x, z }) => new Promise<ArrayBuffer | null>(resolve => requests.set(x + "/" + z, resolve)));
      want(regions, []);
      const buffers = await Promise.race([stopped, Promise.all(batch)]);
      want([], regions);
      requests.clear();
      for (const [i, buffer] of buffers.entries()) {
        if (!buffer) continue;
        const { sections } = decodeRegion(buffer, input.states);
        if (!sections.length) continue;
        for (const { position: [x, , z] } of sections) {
          const bit = (z & 31) * 32 + (x & 31);
          visible[8 + (bit >> 3)] |= 1 << (bit & 7);
        }
        const { x, z } = regions[i];
        parts.push(new Blob([
          `<script type="application/octet-stream" data-dimension="0" data-r2-region="${x}/${z}">`,
          await compressed(buffer),
          "</script>\n",
        ]));
        written++;
      }
      if (visible.subarray(8).some(byte => byte !== 0)) maps.push(visible);
      progress(written);
    }
    if (!written) throw Error("当前维度没有可导出的静态方块，或数据源未提供区块占用表");
    parts.push('<script type="application/octet-stream" id="r2-chunks-0">', await compressed(await new Blob(maps).arrayBuffer()), '</script>\n');
    parts.push('<script type="application/json" id="r2-world">', jsonScript({
      version: 1, name: input.name, camera: input.camera, settings: input.settings, ocean: input.ocean,
      dimensions: [{ key: 0, id: input.dimension }],
    }), '</script>\n', after);
    await Promise.race([stopped, Promise.resolve()]);
    return new Blob(parts, { type: "text/html;charset=utf-8" });
  } finally {
    signal.removeEventListener("abort", abort);
    source.close();
  }
}
