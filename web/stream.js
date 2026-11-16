import { regionKey } from "/r2/scene/prepare.js";

/** 应用调度器：网络、后台准备和 GPU 发布分别限流；核心不感知视距控件或 HTTP 会话。 */
export class WorldStream {
  /** @param {{renderer:Awaited<ReturnType<import("/r2/render/renderer.js").createRenderer>>,world:{id:number},registry:import("/r2/objects/registry.js").ObjectRegistry,onError:(message:string)=>void}} options */
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
  /** 只有跨区块或设置改变才调整近景；远景每跨一个 128 方块区域移动一次目录窗口。 */
  update(position, { nearRadius, farRadius }) {
    if (this.textureChange) return;
    const center = position.map((v) => Math.floor(v / 16)),
      signature = center.join("/") + `/${nearRadius}/${farRadius}`;
    if (signature === this.signature) return;
    this.signature = signature;
    this.center = center;
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
      entry.objectSignature = near ? center.join("/") + "/" + nearRadius : "";
      if (!near && entry.objectsLoaded) {
        const changed = this.objects.removeRegion(key);
        entry.objectsLoaded = "";
        if (changed) this.dirtyNeighbors(entry.x, entry.z);
      }
    }
    this.pump();
  }
  priority(entry) {
    return Math.hypot(
      entry.x * 8 + 4 - this.center[0],
      entry.z * 8 + 4 - this.center[2],
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
          e.prepared < e.version,
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
      if (this.disposed || this.entries.get(entry.key) !== entry) return;
      this.bytes += buffer.byteLength;
      entry.state = "storing";
      this.worker.postMessage({ type: "put", key: entry.key, buffer }, [
        buffer,
      ]);
    } catch (error) {
      if (error.name !== "AbortError" && !this.disposed) {
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
      if (entry) {
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
      } catch (error) { change.reject(error); }
      return;
    }
    if (data.type === "prepared" && this.textureChange) {
      this.preparing = null;
      return;
    }
    if (data.type === "prepared") {
      const pending = this.preparing;
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
   * @param {import("/r2/render/textures.js").TextureSet} set
   */
  async setTextures(set) {
    const change = { set, versions: new Map() };
    change.promise = new Promise((resolve, reject) => Object.assign(change, { resolve, reject }));
    this.textureChange = change;
    this.ready = [];
    for (const entry of this.entries.values()) this.dirty(entry);
    const regions = [...this.gpu.regions.keys()].map((key) => {
      const entry = this.entries.get(key);
      change.versions.set(key, entry.version);
      return { x: entry.x, z: entry.z };
    });
    this.worker.postMessage({ type: "textures", catalog: set.catalog, regions, objects: this.objects.references() });
    try { await change.promise; }
    finally { this.textureChange = null; this.pump(); }
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
