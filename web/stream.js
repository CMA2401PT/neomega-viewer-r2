import { regionKey } from "/r2/index.js";

/** 应用调度器：网络、后台准备和 GPU 发布分别限流；核心不感知视距控件或 HTTP 会话。 */
export class WorldStream {
  /** @param {{renderer:Awaited<ReturnType<import("/r2/index.js").createRenderer>>,world:{id:number},registry:import("/r2/index.js").ObjectRegistry,onError:(message:string)=>void}} options */
  constructor({ renderer, world, registry, onError }) {
    this.renderer = renderer;
    // 越界来源范围取自共享模型，不绑定某一种方块的尺寸。
    this.fragmentRadius = Math.max(
      1,
      Math.ceil(
        Math.max(
          0,
          ...renderer.catalog.models.flatMap((model) =>
            (model.fragments || []).flatMap((part) =>
              part.offset.map(Math.abs),
            ),
          ),
        ) / 128,
      ),
    );
    this.gpu = renderer.createWorld();
    this.objects = renderer.objects;
    this.world = world;
    this.registry = registry;
    this.onError = onError;
    this.entries = new Map();
    this.ready = [];
    this.network = 0;
    this.objectBusy = false;
    this.preparing = null;
    this.serial = 0;
    this.bytes = 0;
    this.clippedSections = world.clippedSections || 0;
    this.uploadMS = 0;
    this.prepareMS = 0;
    this.disposed = false;
    this.controllers = new Set();
    this.worker = new Worker("/r2/scene/worker.js", { type: "module" });
    this.worker.onmessage = (event) => this.message(event.data);
    this.worker.onerror = (event) => onError(event.message);
    this.worker.postMessage({
      type: "init",
      catalog: renderer.catalog,
    });
  }
  near(x, z) {
    const dx = Math.max(
        x * 8 - this.center[0],
        0,
        this.center[0] - (x * 8 + 7),
      ),
      dz = Math.max(z * 8 - this.center[2], 0, this.center[2] - (z * 8 + 7));
    return dx * dx + dz * dz <= this.nearRadius ** 2;
  }
  dirty(entry) {
    entry.version++;
    entry.failed = false;
  }
  dirtyNeighbors(x, z) {
    for (let dz = -this.fragmentRadius; dz <= this.fragmentRadius; dz++)
      for (let dx = -this.fragmentRadius; dx <= this.fragmentRadius; dx++) {
        const entry = this.entries.get(regionKey(x + dx, z + dz));
        if (entry?.visible) this.dirty(entry);
      }
  }
  /** 远景跟随 position，近景对象跟随 nearPosition；两种视图共用同一套区域生命周期。
   * @param {number[]} position
   * @param {{nearRadius:number,farRadius:number,nearPosition?:number[]}} options
   */
  update(position, { nearRadius, farRadius, nearPosition = position }) {
    if (this.textureChange) return;
    const center = nearPosition.map((v) => Math.floor(v / 16)),
      farCenter = position.map((v) => Math.floor(v / 128 + 0.5)),
      signature =
        center.join("/") +
        "/" +
        farCenter.join("/") +
        `/${nearRadius}/${farRadius}`;
    if (signature === this.signature) return;
    this.signature = signature;
    this.center = center;
    this.farCenter = position.map((v) => v / 16);
    this.nearRadius = nearRadius;
    const side = farRadius * 32,
      origin = [
        Math.floor(position[0] / 128 + 0.5) * 128 - side / 2,
        -64,
        Math.floor(position[2] / 128 + 0.5) * 128 - side / 2,
      ];
    this.gpu.setWindow(origin, [side, 512, side]);
    const wanted = new Map();
    for (let z = origin[2] / 128; z < (origin[2] + side) / 128; z++)
      for (let x = origin[0] / 128; x < (origin[0] + side) / 128; x++)
        wanted.set(regionKey(x, z), { x, z, visible: true });
    // 所有静态模型都可能越界；整个可见窗口保留一圈来源区域。
    for (const value of [...wanted.values()])
      for (let dz = -this.fragmentRadius; dz <= this.fragmentRadius; dz++)
        for (let dx = -this.fragmentRadius; dx <= this.fragmentRadius; dx++) {
          const x = value.x + dx,
            z = value.z + dz,
            key = regionKey(x, z);
          if (!wanted.has(key)) wanted.set(key, { x, z, visible: false });
        }
    // 地图移到远处时仍保留主相机附近的来源与对象，收起地图无需重新加载对象。
    for (
      let z = Math.floor((center[2] - nearRadius) / 8);
      z <= Math.floor((center[2] + nearRadius) / 8);
      z++
    )
      for (
        let x = Math.floor((center[0] - nearRadius) / 8);
        x <= Math.floor((center[0] + nearRadius) / 8);
        x++
      ) {
        const key = regionKey(x, z);
        if (this.near(x, z) && !wanted.has(key))
          wanted.set(key, { x, z, visible: false });
      }
    const dropped = [];
    for (const [key, entry] of this.entries)
      if (!wanted.has(key)) {
        entry.controller?.abort();
        this.gpu.remove(key);
        this.objects.removeRegion(key);
        this.entries.delete(key);
        dropped.push(key);
        this.dirtyNeighbors(entry.x, entry.z);
      }
    if (dropped.length)
      this.worker.postMessage({ type: "drop", keys: dropped });
    for (const [key, value] of wanted) {
      let entry = this.entries.get(key);
      if (!entry) {
        entry = {
          ...value,
          key,
          state: "queued",
          request: 0,
          contentVersion: 0,
          requiredVersion: 0,
          version: 1,
          published: 0,
          prepared: 0,
          near: false,
          objectSignature: "",
          objectsLoaded: "",
          failed: false,
        };
        this.entries.set(key, entry);
      }
      const near = this.near(value.x, value.z);
      if (entry.visible !== value.visible) this.dirty(entry);
      if (entry.visible && !value.visible) this.gpu.remove(key);
      entry.visible = value.visible;
      entry.near = near;
      entry.objectSignature = near ? center.join("/") + "/" + nearRadius + "/" + entry.contentVersion : "";
      if (!near && entry.objectsLoaded) {
        const changed = this.objects.removeRegion(key);
        entry.objectsLoaded = "";
        if (changed) this.dirtyNeighbors(entry.x, entry.z);
      }
    }
    this.pump();
  }
  /** 失效只替换来源字节，旧 GPU 内容保留到新结果准备完成。
   * @param {number[][]} regions @param {number} version
   */
  invalidateRegions(regions, version) {
    for (const [x, z] of regions) {
      const entry = this.entries.get(regionKey(x, z));
      if (!entry) continue;
      entry.controller?.abort();
      entry.request = ++this.serial;
      entry.requiredVersion = version;
      entry.contentVersion++;
      entry.objectSignature = entry.near ? this.center.join("/") + "/" + this.nearRadius + "/" + entry.contentVersion : "";
      entry.state = "queued";
      this.dirty(entry);
      this.dirtyNeighbors(x, z);
    }
    this.pump();
  }
  /** SSE 重连只重新同步保留的区域，不改变相机。 */
  invalidateAll(version) {
    this.invalidateRegions([...this.entries.values()].map(e => [e.x, e.z]), version);
  }
  /** 动态来源等待失效邻区，避免把旧片段标成新版本；静态来源仍随到随准备。 */
  sourcesReady(entry) {
    if (this.world.kind !== "connection") return true;
    for (let dz = -this.fragmentRadius; dz <= this.fragmentRadius; dz++)
      for (let dx = -this.fragmentRadius; dx <= this.fragmentRadius; dx++) {
        const source = this.entries.get(regionKey(entry.x + dx, entry.z + dz));
        if (source && source.state !== "stored") return false;
      }
    return true;
  }
  /** 按相同行合并实际保留的区域，来源无需重算地图与模型边界。 */
  interestRanges() {
    const rows = new Map();
    for (const entry of this.entries.values()) {
      if (!rows.has(entry.z)) rows.set(entry.z, []);
      rows.get(entry.z).push(entry.x);
    }
    const ranges = [];
    for (const [z, xs] of rows) {
      xs.sort((a, b) => a - b);
      let start = xs[0], end = start;
      for (const x of xs.slice(1)) {
        if (x === end + 1) end = x;
        else { ranges.push({minX:start*8,minZ:z*8,maxX:end*8+7,maxZ:z*8+7}); start = end = x; }
      }
      ranges.push({minX:start*8,minZ:z*8,maxX:end*8+7,maxZ:z*8+7});
    }
    return ranges;
  }
  priority(entry) {
    return Math.hypot(
      entry.x * 8 + 4 - this.farCenter[0],
      entry.z * 8 + 4 - this.farCenter[2],
    );
  }
  pump() {
    if (this.disposed || this.textureChange || !this.center) return;
    const ordered = [...this.entries.values()].sort(
      (a, b) => this.priority(a) - this.priority(b),
    );
    for (const entry of ordered) {
      if (this.network >= 4) break;
      if (entry.state !== "queued") continue;
      this.download(entry);
    }
    if (!this.preparing && !this.ready.length) {
      const entry = ordered.find(
        (e) =>
          e.visible &&
          e.state === "stored" &&
          !e.failed &&
          e.prepared < e.version &&
          this.sourcesReady(e),
      );
      if (entry) {
        const request = ++this.serial;
        this.preparing = {
          request,
          key: entry.key,
          entry,
          version: entry.version,
        };
        this.worker.postMessage({
          type: "prepare",
          request,
          x: entry.x,
          z: entry.z,
          objects: this.objects.references(),
        });
      }
    }
    if (!this.objectBusy) {
      const entry = ordered.find(
        (e) =>
          e.near &&
          e.state === "stored" &&
          !e.failed &&
          e.objectsLoaded !== e.objectSignature,
      );
      if (entry) this.downloadObjects(entry);
    }
  }
  async download(entry) {
    const controller = new AbortController();
    const request = ++this.serial;
    entry.request = request;
    entry.controller = controller;
    this.controllers.add(controller);
    entry.state = "loading";
    this.network++;
    try {
      const response = await fetch(
        `/api/world/region?world=${this.world.id}&x=${entry.x}&z=${entry.z}`,
        { signal: controller.signal },
      );
      if (!response.ok) throw Error(await response.text());
      const buffer = await response.arrayBuffer();
      if (this.disposed || this.entries.get(entry.key) !== entry || entry.request !== request) return;
      if (Number(response.headers.get("X-R2-Version")) < entry.requiredVersion) { entry.state = "queued"; return; }
      this.bytes += buffer.byteLength;
      this.clippedSections = Math.max(this.clippedSections, Number(response.headers.get("X-R2-Clipped-Sections")));
      entry.state = "storing";
      this.worker.postMessage({ type: "put", key: entry.key, request, buffer }, [
        buffer,
      ]);
    } catch (error) {
      if (error.name !== "AbortError" && !this.disposed && entry.request === request) {
        entry.failed = true;
        entry.state = "failed";
        this.onError(String(error));
      }
    } finally {
      this.network--;
      this.controllers.delete(controller);
      this.pump();
    }
  }
  async downloadObjects(entry) {
    this.objectBusy = true;
    const signature = entry.objectSignature,
      center = [...this.center],
      controller = new AbortController();
    this.controllers.add(controller);
    try {
      const query = new URLSearchParams({
          world: this.world.id,
          x: entry.x,
          z: entry.z,
          cx: center[0],
          cy: center[1],
          cz: center[2],
          radius: this.nearRadius,
        }),
        response = await fetch("/api/world/objects?" + query, {
          signal: controller.signal,
        });
      if (!response.ok) throw Error(await response.text());
      const descriptors = await response.json();
      await this.textureChange?.promise;
      if (
        this.disposed ||
        this.entries.get(entry.key) !== entry ||
        entry.objectSignature !== signature
      )
        return;
      const changed = await this.objects.setRegion(
        entry.key,
        descriptors,
        this.registry,
      );
      if (this.disposed) return;
      if (
        this.entries.get(entry.key) !== entry ||
        entry.objectSignature !== signature
      ) {
        this.objects.removeRegion(entry.key);
        this.dirtyNeighbors(entry.x, entry.z);
        return;
      }
      entry.objectsLoaded = signature;
      if (changed) this.dirtyNeighbors(entry.x, entry.z);
    } catch (error) {
      if (error.name !== "AbortError" && !this.disposed) {
        entry.failed = true;
        this.onError(String(error));
      }
    } finally {
      this.objectBusy = false;
      this.controllers.delete(controller);
      this.pump();
    }
  }
  message(data) {
    if (this.disposed) return;
    if (data.type === "stored") {
      const entry = this.entries.get(data.key);
      if (entry && entry.request === data.request) {
        entry.state = "stored";
        this.dirtyNeighbors(entry.x, entry.z);
      }
    }
    if (data.type === "textures") {
      const change = this.textureChange;
      try {
        for (const result of data.results) this.gpu.install(result);
        this.renderer.setTextures(change.set);
        for (const [key, version] of change.versions) {
          const entry = this.entries.get(key);
          entry.prepared = version;
          entry.published = version;
        }
        change.resolve();
      } catch (error) {
        change.reject(error);
      }
      return;
    }
    if (data.type === "prepared" && this.textureChange) {
      this.preparing = null;
      return;
    }
    if (data.type === "prepared") {
      const pending = this.preparing;
      if (!pending || pending.request !== data.request) return;
      this.preparing = null;
      const entry = this.entries.get(pending.key);
      if (
        entry === pending.entry &&
        entry?.version === pending.version &&
        entry.visible
      ) {
        entry.prepared = pending.version;
        this.ready.push({
          entry,
          version: pending.version,
          result: data.result,
        });
        this.prepareMS += data.result.stats.prepareMS;
      }
    }
    if (data.type === "error" && this.textureChange) {
      this.textureChange.reject(Error(data.error));
      return;
    }
    if (data.type === "error") {
      const entry = this.entries.get(this.preparing?.key || data.key);
      if (entry) entry.failed = true;
      this.preparing = null;
      this.onError(data.error);
    }
    this.pump();
  }
  /** 在绘制前发布一个完成的区域，上传不再阻塞网络下载或后台构建。 */
  publish() {
    if (this.disposed || this.textureChange) return;
    const next = this.ready.shift();
    if (
      next &&
      this.entries.get(next.entry.key) === next.entry &&
      next.entry.version === next.version
    ) {
      try {
        const start = performance.now();
        this.gpu.install(next.result);
        this.uploadMS += performance.now() - start;
        next.entry.published = next.version;
      } catch (error) {
        next.entry.failed = true;
        this.onError(String(error));
      }
    }
    this.pump();
  }
  /** 用缓存来源按新透明分类重新准备已显示区域，完成后同帧发布贴图和空间目录。
   * @param {import("/r2/index.js").TextureSet} set
   */
  async setTextures(set) {
    const change = { set, versions: new Map() };
    change.promise = new Promise((resolve, reject) =>
      Object.assign(change, { resolve, reject }),
    );
    this.textureChange = change;
    this.ready = [];
    for (const entry of this.entries.values()) this.dirty(entry);
    const regions = [...this.gpu.regions.keys()].map((key) => {
      const entry = this.entries.get(key);
      change.versions.set(key, entry.version);
      return { x: entry.x, z: entry.z };
    });
    this.worker.postMessage({
      type: "textures",
      catalog: set.catalog,
      regions,
      objects: this.objects.references(),
    });
    try {
      await change.promise;
    } finally {
      this.textureChange = null;
      this.pump();
    }
  }
  get stats() {
    const all = [...this.entries.values()],
      entries = all.filter((e) => e.visible),
      installed = [...this.gpu.regions.values()];
    return {
      wanted: entries.length,
      loaded: this.gpu.regions.size,
      pending: all.filter(
        (e) =>
          !e.failed &&
          (e.state !== "stored" || (e.visible && e.published !== e.version)),
      ).length,
      failed: all.filter((e) => e.failed).length,
      nearPending: all.filter(
        (e) =>
          e.near &&
          ((e.visible && e.published !== e.version) ||
            e.objectsLoaded !== e.objectSignature),
      ).length,
      sections: installed.reduce((n, e) => n + e.stats.sections, 0),
      bytes: this.bytes,
      clippedSections: this.clippedSections,
      usedBytes: this.gpu.allocator.used * 4,
      prepareMS: this.prepareMS,
      uploadMS: this.uploadMS,
      objects: this.objects.objects.size,
    };
  }
  dispose() {
    this.disposed = true;
    this.textureChange?.reject(Error("存档已经关闭或切换"));
    for (const controller of this.controllers) controller.abort();
    this.worker.terminate();
    this.objects.clear();
    this.ready = [];
  }
}
