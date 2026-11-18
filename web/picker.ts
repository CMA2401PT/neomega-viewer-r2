// 存档与材质包共用服务端目录浏览器；Electron 也可提交用户拖入的本机路径。
import { dimensionName, type OpenWorld, type Progress, type ViewerWorld } from "./types.js";

type Kind = "world" | "pack";
interface Entry { name: string; path: string; kind: string; directory: boolean; openable: boolean }
interface Listing { path: string; parent: string; selected: string; kind: string; roots: Entry[]; entries: Entry[]; nativeDrop: boolean }
interface PickerOptions { kind?: Kind; choose?: OpenWorld; world?: Partial<ViewerWorld> }

declare global {
  interface Window { desktopFiles?: { pathForFile(file: File): string } }
}

/** 文件选择与打开分开，让用户在转换前选定维度、修复和预算。 */
export function createPicker(openWorld: OpenWorld) {
  const dialog = document.querySelector<HTMLDialogElement>("#files")!,
    input = document.querySelector<HTMLInputElement>("#directory")!,
    filter = document.querySelector<HTMLInputElement>("#file-search")!,
    nativeDropButton = document.querySelector<HTMLButtonElement>("#local-drop")!,
    list = document.querySelector<HTMLElement>("#file-list")!,
    message = document.querySelector<HTMLOutputElement>("#file-message")!,
    dimension = document.querySelector<HTMLSelectElement>("#source-dimension")!;
  let listing: Listing, selected = "", kind: Kind = "world", onChoose = openWorld;
  let currentWorld: Partial<ViewerWorld> = {};
  let opening: AbortController | null = null, browsing: AbortController | null = null;
  let dropping: AbortController | null = null;
  const dropZone = document.querySelector<HTMLElement>("#file-drop")!;
  const desktopFiles = window.desktopFiles;
  dropZone.hidden = typeof desktopFiles?.pathForFile !== "function";
  const labels: Record<string, string> = {
    directory: "Java 存档", bedrock: "Bedrock 存档", folder: "目录",
    pack: "Java PackFS", texture: "材质包", level: "存档入口",
    schem: "Java 结构 · schem", schematic: "Java 结构 · schematic",
    litematic: "Java 结构 · litematic", nbt: "Java 结构 · nbt",
    mcstructure: "Bedrock 结构", mcworld: "Bedrock 世界包",
  };
  /** Bedrock 固定三个维度，Java 首次打开主世界，再使用服务端扫描的列表。 */
  function setDimensions(values: string[] = [""]) {
    dimension.replaceChildren(...values.map((value) => {
      const option = document.createElement("option");
      option.value = value;
      option.textContent = dimensionName(value);
      return option;
    }));
  }
  /** 浏览错误留在选择器中，用户可以修改路径继续。 */
  async function browse(path = "") {
    if (opening) return false;
    browsing?.abort();
    const controller = new AbortController();
    browsing = controller;
    message.textContent = "正在读取目录…";
    input.disabled = true;
    document.querySelector<HTMLButtonElement>("#open-directory")!.disabled = true;
    document.querySelector<HTMLButtonElement>("#parent")!.disabled = true;
    try {
      const response = await fetch("/api/files?kind=" + kind + "&path=" + encodeURIComponent(path), { signal: controller.signal });
      if (!response.ok) throw Error(await response.text());
      const result: Listing = await response.json();
      if (controller.signal.aborted || !dialog.open) return false;
      listing = result;
      nativeDropButton.hidden = !listing.nativeDrop;
      selected = listing.selected;
      input.value = selected || listing.path;
      setDimensions(["bedrock", "mcstructure", "mcworld"].includes(listing.kind)
        ? ["0", "1", "2"]
        : input.value === currentWorld.path ? currentWorld.dimensions : [""]);
      dimension.value = input.value === currentWorld.path ? currentWorld.dimension ?? "" : dimension.options[0].value;
      list.replaceChildren();
      document.querySelector<HTMLElement>("#roots")!.replaceChildren();
      for (const root of listing.roots) {
        const button = document.createElement("button");
        button.textContent = root.name;
        button.onclick = () => browse(root.path);
        document.querySelector<HTMLElement>("#roots")!.append(button);
      }
      for (const entry of listing.entries) {
        const row = document.createElement("button");
        row.className = "file";
        row.setAttribute("aria-selected", String(entry.path === selected));
        const name = document.createElement("span"), label = document.createElement("span");
        name.textContent = entry.name;
        label.className = "kind";
        label.textContent = labels[entry.kind] || entry.kind;
        row.append(name, label);
        row.onclick = () => {
          if (!entry.openable) return browse(entry.path);
          selected = input.value = entry.path;
          for (const other of list.children) other.setAttribute("aria-selected", String(other === row));
          const sourceKind = entry.kind === "level" ? listing.kind : entry.kind;
          setDimensions(["bedrock", "mcstructure", "mcworld"].includes(sourceKind)
            ? ["0", "1", "2"] : entry.path === currentWorld.path ? currentWorld.dimensions : [""]);
          message.textContent = "已选择 " + entry.name;
        };
        row.ondblclick = () => entry.directory ? browse(entry.path) : choose(entry.path);
        list.append(row);
      }
      filter.value = "";
      message.textContent = listing.entries.length + " 个项目";
      return true;
    } catch (error) {
      if (!(error instanceof Error && error.name === "AbortError")) message.textContent = String(error);
      return false;
    }
    finally {
      if (browsing === controller) {
        browsing = null;
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
    message.textContent = kind === "pack" ? "正在合成材质包…" : "正在打开来源…";
    opening = new AbortController();
    document.querySelector<HTMLButtonElement>("#open-directory")!.disabled = true;
    try {
      if (kind === "pack") await onChoose(path);
      else await onChoose(path, {
        dimension: dimension.value,
        bedrockFix: document.querySelector<HTMLInputElement>("#source-fix")!.checked,
        structureMemoryMiB: Number(document.querySelector<HTMLInputElement>("#source-memory")!.value),
        structureWorkers: Number(document.querySelector<HTMLInputElement>("#source-workers")!.value),
      }, progress, opening.signal);
      opening = null;
      dialog.close();
    } catch (error) {
      if (!(error instanceof Error && error.name === "AbortError")) message.textContent = String(error);
    } finally {
      opening = null;
      document.querySelector<HTMLButtonElement>("#open-directory")!.disabled = false;
    }
  }
  dialog.addEventListener("close", () => {
    if (dialog.open) return;
    opening?.abort();
    browsing?.abort();
    dropping?.abort();
    dropZone.classList.remove("drag-over");
  });
  filter.addEventListener("input", () => {
    const query = filter.value.trim().toLocaleLowerCase();
    for (const row of list.children) row.toggleAttribute("hidden", !row.textContent!.toLocaleLowerCase().includes(query));
  });
  async function openDroppedPath(getPath: (signal: AbortSignal) => string | Promise<string>) {
    if (!dialog.open || opening || dropping) return;
    const controller = new AbortController();
    dropping = controller;
    nativeDropButton.disabled = true;
    dropZone.setAttribute("aria-busy", "true");
    try {
      const path = await getPath(controller.signal);
      if (controller.signal.aborted || !dialog.open) return;
      if (!path) { message.textContent = "已取消"; return; }
      const chosenDimension = dimension.value;
      if (!await browse(path) || controller.signal.aborted || !dialog.open) return;
      if (listing.kind === "folder" || listing.kind === "unknown") {
        message.textContent = "这不是可打开的存档或材质包。";
        return;
      }
      if ([...dimension.options].some((option) => option.value === chosenDimension)) dimension.value = chosenDimension;
      await choose(input.value);
    } catch (error) {
      if (!controller.signal.aborted && dialog.open) message.textContent = String(error);
    } finally {
      if (dropping === controller) {
        dropping = null;
        nativeDropButton.disabled = false;
        dropZone.removeAttribute("aria-busy");
      }
    }
  }
  nativeDropButton.onclick = () => openDroppedPath(async (signal) => {
    message.textContent = "等待从资源管理器拖入…";
    const response = await fetch("/api/files/local-drop", { method: "POST", signal });
    if (!response.ok) throw Error(await response.text());
    const { path } = await response.json() as { path: string };
    return path;
  });
  dropZone.addEventListener("dragover", (event) => {
    event.preventDefault();
    if (event.dataTransfer) event.dataTransfer.dropEffect = opening || dropping ? "none" : "copy";
    if (!opening && !dropping) dropZone.classList.add("drag-over");
  });
  dropZone.addEventListener("dragleave", () => dropZone.classList.remove("drag-over"));
  dropZone.addEventListener("drop", (event) => {
    event.preventDefault();
    dropZone.classList.remove("drag-over");
    if (opening || dropping || !desktopFiles || !event.dataTransfer) return;
    const files = [...event.dataTransfer.files];
    if (files.length !== 1) {
      message.textContent = "请一次拖入一个目录或文件。";
      return;
    }
    void openDroppedPath(() => {
      const path = desktopFiles.pathForFile(files[0]);
      if (!path) throw Error("无法读取路径，请从文件资源管理器拖入本地目录或文件。");
      return path;
    });
  });
  document.querySelector<HTMLButtonElement>("#browse")!.onclick = () => browse(input.value);
  input.addEventListener("input", () => { setDimensions([""]); });
  input.addEventListener("keydown", (event) => {
    if (event.key === "Enter") { event.preventDefault(); browse(input.value); }
  });
  document.querySelector<HTMLButtonElement>("#parent")!.onclick = () => browse(listing.parent);
  document.querySelector<HTMLButtonElement>("#open-directory")!.onclick = () => choose(input.value);
  return ({ kind: nextKind = "world", choose: callback = openWorld, world = {} }: PickerOptions = {}) => {
    dropping?.abort();
    kind = nextKind;
    currentWorld = world;
    onChoose = callback;
    setDimensions(world.dimensions || [""]);
    dimension.value = world.dimension || "";
    document.querySelector<HTMLInputElement>("#source-fix")!.checked = world.bedrockFix ?? true;
    document.querySelector<HTMLInputElement>("#source-memory")!.value = String(world.structureMemoryMiB || 512);
    document.querySelector<HTMLInputElement>("#source-workers")!.value = String(world.structureWorkers || 0);
    document.querySelector<HTMLElement>("#source-options")!.hidden = kind === "pack";
    nativeDropButton.textContent = kind === "pack" ? "从资源管理器拖入材质包" : "从资源管理器拖入存档或结构";
    document.querySelector<HTMLElement>("#file-drop-title")!.textContent = kind === "pack"
      ? "将材质包 ZIP 或目录拖到这里使用"
      : "将存档目录或文件拖到这里打开";
    dialog.querySelector("h2")!.textContent = kind === "pack" ? "选择 Java 材质包" : "打开存档或结构";
    document.querySelector<HTMLButtonElement>("#open-directory")!.textContent = kind === "pack" ? "使用材质包" : "打开所选来源";
    document.querySelector<HTMLElement>("#source-hint")!.textContent = kind === "pack"
      ? "选择 zip 或包含 assets 的目录，也可直接输入路径。"
      : "选择文件后点击打开；双击存档目录可浏览内部。结构文件转换到旁边的同名目录。";
    dialog.showModal();
    browse();
  };
}
