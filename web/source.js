// 应用的 HTTP 数据源：按会话的需要向 Go 服务拉取区域与近景对象。存档和内置场景都走这里。

/** 同时在途的区域请求数；对象请求一次一个。 */
const REGION_REQUESTS = 4;

/** @typedef {import("/r2/index.js").DataSource} DataSource */
/** @typedef {import("/r2/index.js").WorldSink} WorldSink */

/** 区域内容在一个会话里不变，所以每个区域只拉一次；移出需要的范围时取消在途请求。
 * @param {number} worldID 服务端的会话编号，世界切换后旧请求得到 409
 * @returns {DataSource & {clippedSections:number}}
 */
export function httpSource(worldID) {
  /** @type {WorldSink} */
  let sink;
  let closed = false;
  const key = (x, z) => `${x}/${z}`;
  /** 等待拉取的区域。 @type {Map<string,{x:number,z:number}>} */
  const regionQueue = new Map(),
    /** @type {Map<string,AbortController>} */
    regionActive = new Map(),
    /** @type {Map<string,{x:number,z:number,center:number[],radius:number}>} */
    objectQueue = new Map();
  /** @type {{key:string,controller:AbortController}|null} */
  let objectActive = null;
  /** 会话最近给出的远景中心，排队时先拉离它近的区域。 */
  let center = [0, 0];
  /** @param {Map<string,{x:number,z:number}>} queue */
  const nearest = (queue) => {
    let best = null, distance = Infinity;
    for (const entry of queue) {
      const d = Math.hypot(entry[1].x * 128 + 64 - center[0], entry[1].z * 128 + 64 - center[1]);
      if (d < distance) [best, distance] = [entry, d];
    }
    return best;
  };
  const source = {
    /** 服务端统计的超出可显示高度的 section 数。 */
    clippedSections: 0,
    open(value) {
      sink = value;
    },
    want({ center: focus, regions, objects }) {
      center = focus;
      for (const { x, z } of regions.remove) {
        const k = key(x, z);
        regionQueue.delete(k);
        regionActive.get(k)?.abort();
        regionActive.delete(k);
      }
      for (const value of regions.add) regionQueue.set(key(value.x, value.z), value);
      for (const { x, z } of objects.remove) {
        const k = key(x, z);
        objectQueue.delete(k);
        if (objectActive?.key === k) objectActive.controller.abort();
      }
      for (const value of objects.add) {
        const k = key(value.x, value.z);
        // 圆心变了：在途的旧请求作废，按新圆心重拉。
        if (objectActive?.key === k) objectActive.controller.abort();
        objectQueue.delete(k);
        objectQueue.set(k, value);
      }
      pump();
    },
    close() {
      closed = true;
      for (const controller of regionActive.values()) controller.abort();
      objectActive?.controller.abort();
      regionQueue.clear();
      objectQueue.clear();
    },
  };
  /** @param {string} path @param {Record<string,number>} query @param {AbortSignal} signal */
  async function get(path, query, signal) {
    const response = await fetch(`${path}?${new URLSearchParams({ world: String(worldID), ...query })}`, { signal });
    if (!response.ok) throw Error(await response.text());
    const clipped = Number(response.headers.get("X-R2-Clipped-Sections"));
    if (clipped > source.clippedSections) source.clippedSections = clipped;
    return response;
  }
  function pump() {
    if (closed) return;
    while (regionQueue.size && regionActive.size < REGION_REQUESTS) {
      const [k, { x, z }] = nearest(regionQueue);
      regionQueue.delete(k);
      const controller = new AbortController();
      regionActive.set(k, controller);
      get("/api/world/region", { x, z }, controller.signal)
        .then((response) => response.arrayBuffer())
        .then(
          (buffer) => !controller.signal.aborted && sink.region(x, z, buffer),
          (error) => !controller.signal.aborted && sink.error(x, z, error),
        )
        .finally(() => {
          if (regionActive.get(k) === controller) regionActive.delete(k);
          pump();
        });
    }
    if (!objectActive && objectQueue.size) {
      const [k, { x, z, center: around, radius }] = nearest(objectQueue);
      objectQueue.delete(k);
      const controller = new AbortController();
      objectActive = { key: k, controller };
      get("/api/world/objects", { x, z, cx: around[0], cy: around[1], cz: around[2], radius }, controller.signal)
        .then((response) => response.json())
        .then(
          (descriptors) => !controller.signal.aborted && sink.objects(x, z, descriptors),
          (error) => !controller.signal.aborted && sink.error(x, z, error),
        )
        .finally(() => {
          if (objectActive?.controller === controller) objectActive = null;
          pump();
        });
    }
  }
  return source;
}
