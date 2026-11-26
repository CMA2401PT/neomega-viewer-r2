// 存档从服务端目录浏览器选择。
import { dimensionName, type OpenWorld, type Progress, type ViewerWorld } from "./types.js";

interface Entry { name: string; path: string; kind: string; directory: boolean; openable: boolean }
interface Listing { path: string; parent: string; selected: string; kind: string; roots: Entry[]; entries: Entry[] }
interface PickerOptions { world?: Partial<ViewerWorld> }

/** 文件选择与打开分开，让用户在转换前选定维度、修复和预算。 */
export function createPicker(openWorld: OpenWorld) {
  const dialog = document.querySelector<HTMLDialogElement>("#files")!,
    input = document.querySelector<HTMLInputElement>("#directory")!,
    search = document.querySelector<HTMLInputElement>("#file-search")!,
    list = document.querySelector<HTMLElement>("#file-list")!,
    message = document.querySelector<HTMLOutputElement>("#file-message")!,
    dimension = document.querySelector<HTMLSelectElement>("#source-dimension")!;
  let listing: Listing, selected = "", lastPath = "";
  let currentWorld: Partial<ViewerWorld> = {};
  let opening: AbortController | null = null, browsing: AbortController | null = null;
  const destination = document.querySelector<HTMLOutputElement>("#structure-root")!;
  const resetRoot = document.querySelector<HTMLButtonElement>("#reset-structure-root")!;
  const rootKey = "neomega-r2-structure-root";
  function showPath(path: string): void {
    // 长路径优先显示末尾，让窄屏也能看清当前目录；手动输入时保留用户的滚动位置。
    input.value = path;
    input.scrollLeft = input.scrollWidth;
  }
  function showRoot(path: string) {
    destination.value = path || "源文件旁";
    resetRoot.disabled = !path;
  }
  function setRoot(path: string) {
    if (path) localStorage.setItem(rootKey, path);
    else localStorage.removeItem(rootKey);
    showRoot(path);
    message.textContent = path ? "转换输出目录：" + path : "转换结果将放在源文件旁。";
  }
  resetRoot.onclick = () => setRoot("");
  const labels: Record<string, string> = {
    directory: "Java 存档", bedrock: "Bedrock 存档", folder: "目录",
    pack: "Java PackFS", texture: "材质包", level: "存档入口",
    schem: "Java 结构 · schem", schematic: "Java 结构 · schematic",
    litematic: "Java 结构 · litematic", nbt: "Java 结构 · nbt",
    mcstructure: "Bedrock 结构", mcworld: "Bedrock 世界包",
  };
  // 列表图标只区分三类：可直接打开的存档、需要转换的结构文件、普通目录和其他文件。
  const icons: Record<string, string> = {
    directory: "i-world", bedrock: "i-world", level: "i-world", mcworld: "i-world", pack: "i-world",
    schem: "i-structure", schematic: "i-structure", litematic: "i-structure", nbt: "i-structure", mcstructure: "i-structure",
    folder: "i-folder", texture: "i-image",
  };
  function icon(name: string) {
    const svg = document.createElementNS("http://www.w3.org/2000/svg", "svg"),
      use = document.createElementNS("http://www.w3.org/2000/svg", "use");
    svg.setAttribute("aria-hidden", "true");
    use.setAttribute("href", "#" + name);
    svg.append(use);
    return svg;
  }
  /** Bedrock 固定三个维度，Java 首次打开主世界，再使用服务端扫描的列表。 */
  function setDimensions(values: string[] = [""]) {
    dimension.replaceChildren(...values.map((value) => {
      const option = document.createElement("option");
      option.value = value;
      option.textContent = dimensionName(value);
      return option;
    }));
  }
  // 只匹配当前列表中的名称，避免类型标签也被当成文件名命中。
  search.oninput = () => {
    const query = search.value.trim().toLowerCase();
    let matches = 0;
    for (const row of list.querySelectorAll<HTMLElement>(".file-row")) {
      row.hidden = !row.dataset.name!.includes(query);
      if (!row.hidden) matches++;
    }
    message.textContent = query ? `匹配 ${matches} / ${list.childElementCount} 个项目` : `${matches} 个项目`;
  };
  /** 浏览错误留在选择器中，用户可以修改路径继续。 */
  async function browse(path = "") {
    if (opening) return false;
    browsing?.abort();
    const controller = new AbortController();
    browsing = controller;
    message.textContent = "正在读取目录…";
    search.disabled = true;
    input.disabled = true;
    document.querySelector<HTMLButtonElement>("#open-directory")!.disabled = true;
    document.querySelector<HTMLButtonElement>("#parent")!.disabled = true;
    try {
      const response = await fetch("/api/files?kind=world&path=" + encodeURIComponent(path), { signal: controller.signal });
      if (!response.ok) throw Error(await response.text());
      listing = await response.json();
      selected = listing.selected;
      showPath(selected || listing.path);
      setDimensions(["bedrock", "mcstructure", "mcworld"].includes(listing.kind)
        ? ["0", "1", "2"]
        : input.value === currentWorld.path ? currentWorld.dimensions : [""]);
      dimension.value = input.value === currentWorld.path ? currentWorld.dimension ?? "" : dimension.options[0].value;
      search.value = "";
      list.replaceChildren();
      document.querySelector<HTMLElement>("#roots")!.replaceChildren();
      for (const root of listing.roots) {
        const button = document.createElement("button");
        button.className = "chip";
        button.textContent = root.name;
        button.onclick = () => browse(root.path);
        document.querySelector<HTMLElement>("#roots")!.append(button);
      }
      for (const entry of listing.entries) {
        const item = document.createElement("div");
        item.className = "file-row";
        item.dataset.name = entry.name.toLowerCase();
        const row = document.createElement("button");
        row.className = "file";
        row.setAttribute("aria-selected", String(entry.path === selected));
        const name = document.createElement("span");
        name.textContent = entry.name;
        name.className = "name";
        row.append(icon(icons[entry.kind] ?? "i-file"), name);
        if (entry.kind !== "folder") {
          const label: HTMLSpanElement = document.createElement("span");
          label.className = "kind";
          label.textContent = labels[entry.kind] || entry.kind;
          row.append(label);
        }
        row.onclick = () => {
          if (!entry.openable) return browse(entry.path);
          selected = entry.path;
          showPath(entry.path);
          for (const other of list.querySelectorAll(".file")) other.setAttribute("aria-selected", String(other === row));
          const sourceKind = entry.kind === "level" ? listing.kind : entry.kind;
          setDimensions(["bedrock", "mcstructure", "mcworld"].includes(sourceKind)
            ? ["0", "1", "2"] : entry.path === currentWorld.path ? currentWorld.dimensions : [""]);
          message.textContent = "已选择 " + entry.name;
        };
        row.ondblclick = () => entry.directory ? browse(entry.path) : choose(entry.path);
        item.append(row);
        if (entry.directory) {
          const use = document.createElement("button");
          use.className = "use-output-dir icon-button";
          use.title = "设为转换输出目录";
          use.setAttribute("aria-label", `把 ${entry.name} 设为转换输出目录`);
          use.append(icon("i-outdir"));
          use.onclick = () => setRoot(entry.path);
          item.append(use);
        }
        list.append(item);
      }
      message.textContent = listing.entries.length + " 个项目";
      return true;
    } catch (error) {
      if (!(error instanceof Error && error.name === "AbortError")) message.textContent = String(error);
      return false;
    }
    finally {
      if (browsing === controller) {
        browsing = null;
        search.disabled = false;
        input.disabled = false;
        document.querySelector<HTMLButtonElement>("#open-directory")!.disabled = false;
        document.querySelector<HTMLButtonElement>("#parent")!.disabled = !listing;
      }
    }
  }
  /** 进度来自转换器的阶段，不能把读取字节进度当作整个转换的完成比例。 */
  function progress(value: Progress) {
    const stages: Record<string, string> = {reading:"读取结构", writing:"写出区块", extracting:"解包世界", finishing:"发布存档", complete:"转换完成"};
    message.textContent = `${stages[value.stage] || value.stage} ${value.total ? Math.round(value.completed / value.total * 100) + "%" : value.completed || ""}`;
  }
  /** 保持对话框直到来源准备完成，关闭对话框会取消仍在进行的转换。 */
  async function choose(path: string) {
    if (opening) return;
    browsing?.abort();
    browsing = null;
    message.textContent = "正在打开来源…";
    opening = new AbortController();
    search.disabled = true;
    document.querySelector<HTMLButtonElement>("#open-directory")!.disabled = true;
    try {
      await openWorld(path, {
        dimension: dimension.value,
        structureRoot: localStorage.getItem(rootKey) ?? "",
        bedrockFix: document.querySelector<HTMLInputElement>("#source-fix")!.checked,
        structureMemoryMiB: Number(document.querySelector<HTMLInputElement>("#source-memory")!.value),
        structureWorkers: Number(document.querySelector<HTMLInputElement>("#source-workers")!.value),
      }, progress, opening.signal);
      lastPath = path;
      opening = null;
      dialog.close();
    } catch (error) {
      if (!(error instanceof Error && error.name === "AbortError")) message.textContent = String(error);
    } finally {
      search.disabled = false;
      opening = null;
      document.querySelector<HTMLButtonElement>("#open-directory")!.disabled = false;
    }
  }
  dialog.addEventListener("close", () => {
    opening?.abort();
    browsing?.abort();
  });
  document.querySelector<HTMLButtonElement>("#browse")!.onclick = () => browse(input.value);
  input.addEventListener("input", () => { setDimensions([""]); });
  input.addEventListener("keydown", (event) => {
    if (event.key === "Enter") { event.preventDefault(); browse(input.value); }
  });
  document.querySelector<HTMLButtonElement>("#parent")!.onclick = () => browse(listing.parent);
  document.querySelector<HTMLButtonElement>("#open-directory")!.onclick = () => choose(input.value);
  return ({ world = {} }: PickerOptions = {}) => {
    currentWorld = world;
    showRoot(localStorage.getItem(rootKey) ?? "");
    setDimensions(world.dimensions || [""]);
    dimension.value = world.dimension || "";
    document.querySelector<HTMLInputElement>("#source-fix")!.checked = world.bedrockFix ?? true;
    document.querySelector<HTMLInputElement>("#source-memory")!.value = String(world.structureMemoryMiB || 512);
    document.querySelector<HTMLInputElement>("#source-workers")!.value = String(world.structureWorkers || 0);
    dialog.showModal();
    browse(world.path || lastPath);
  };
}
