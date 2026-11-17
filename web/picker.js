// 存档与材质包共用服务端目录浏览器；Electron 也可提交用户拖入的本机路径。
/** 文件选择与打开分开，让用户在转换前选定维度、修复和预算。
 * @param {(path:string,options?:Object,progress?:(value:Object)=>void,signal?:AbortSignal)=>Promise<void>} openWorld
 */
export function createPicker(openWorld) {
  const dialog = document.querySelector("#files"),
    input = document.querySelector("#directory"),
    list = document.querySelector("#file-list"),
    message = document.querySelector("#file-message"),
    dimension = document.querySelector("#source-dimension"),
    dimensions = dimension;
  let listing, selected = "", kind = "world", onChoose = openWorld, opening, currentWorld = {};
  let browsing, dropping = false;
  const dropZone = document.querySelector("#file-drop");
  const desktopFiles = globalThis.desktopFiles;
  dropZone.hidden = typeof desktopFiles?.pathForFile !== "function";
  const paths = { world: "", pack: "" };
  const labels = {
    directory: "Java 存档", bedrock: "Bedrock 存档", folder: "目录",
    pack: "Java PackFS", texture: "材质包", level: "存档入口",
    schem: "Java 结构 · schem", schematic: "Java 结构 · schematic",
    litematic: "Java 结构 · litematic", nbt: "Java 结构 · nbt",
    mcstructure: "Bedrock 结构", mcworld: "Bedrock 世界包",
  };
  /** Bedrock 固定三个维度，Java 首次打开主世界，再使用服务端扫描的列表。 */
  function setDimensions(values) {
    dimensions.replaceChildren(...values.map((value) => {
      const option = document.createElement("option");
      option.value = value;
      option.textContent = ({"":"主世界（打开后可切换维度）", ".":"主世界", "DIM-1":"下界", "DIM1":"末地", "0":"主世界", "1":"下界", "2":"末地"})[value] || value;
      return option;
    }));
  }
  /** 浏览错误留在选择器中，用户可以修改路径继续。 @param {string} path */
  async function browse(path = "") {
    if (opening) return false;
    browsing?.abort();
    const controller = new AbortController();
    browsing = controller;
    message.textContent = "正在读取目录…";
    input.disabled = true;
    document.querySelector("#open-directory").disabled = true;
    document.querySelector("#parent").disabled = true;
    try {
      const response = await fetch("/api/files?kind=" + kind + "&path=" + encodeURIComponent(path), { signal: controller.signal });
      if (!response.ok) throw Error(await response.text());
      listing = await response.json();
      selected = listing.selected;
      input.value = selected || listing.path;
      setDimensions(["bedrock", "mcstructure", "mcworld"].includes(listing.kind)
        ? ["0", "1", "2"]
        : input.value === currentWorld.path ? currentWorld.dimensions : [""]);
      dimension.value = input.value === currentWorld.path ? currentWorld.dimension : dimension.options[0].value;
      list.replaceChildren();
      document.querySelector("#roots").replaceChildren();
      for (const root of listing.roots) {
        const button = document.createElement("button");
        button.textContent = root.name;
        button.onclick = () => browse(root.path);
        document.querySelector("#roots").append(button);
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
      message.textContent = listing.entries.length + " 个项目";
      return true;
    } catch (error) {
      if (error.name !== "AbortError") message.textContent = String(error);
      return false;
    }
    finally {
      if (browsing === controller) {
        browsing = null;
        input.disabled = false;
        document.querySelector("#open-directory").disabled = false;
        document.querySelector("#parent").disabled = !listing;
      }
    }
  }
  /** 进度来自转换器的阶段，不能把读取字节进度当作整个转换的完成比例。 */
  function progress(value) {
    const stages = {reading:"读取结构", writing:"写出区块", extracting:"解包世界", finishing:"发布存档", complete:"转换完成"};
    message.textContent = `${stages[value.stage] || value.stage} ${value.total ? Math.round(value.completed / value.total * 100) + "%" : value.completed || ""}`;
  }
  /** 保持对话框直到来源准备完成，关闭对话框会取消仍在进行的转换。 @param {string} path */
  async function choose(path) {
    if (opening) return;
    browsing?.abort();
    browsing = null;
    message.textContent = kind === "pack" ? "正在合成材质包…" : "正在打开来源…";
    opening = new AbortController();
    document.querySelector("#open-directory").disabled = true;
    try {
      if (kind === "pack") await onChoose(path);
      else await onChoose(path, {
        dimension: dimension.value,
        bedrockFix: document.querySelector("#source-fix").checked,
        structureMemoryMiB: Number(document.querySelector("#source-memory").value),
        structureWorkers: Number(document.querySelector("#source-workers").value),
      }, progress, opening.signal);
      paths[kind] = path;
      opening = null;
      dialog.close();
    } catch (error) {
      if (error.name !== "AbortError") message.textContent = String(error);
    } finally {
      opening = null;
      document.querySelector("#open-directory").disabled = false;
    }
  }
  dialog.addEventListener("close", () => {
    opening?.abort();
    browsing?.abort();
    dropZone.classList.remove("drag-over");
  });
  dropZone.addEventListener("dragover", (event) => {
    event.preventDefault();
    event.dataTransfer.dropEffect = opening || dropping ? "none" : "copy";
    if (!opening && !dropping) dropZone.classList.add("drag-over");
  });
  dropZone.addEventListener("dragleave", () => dropZone.classList.remove("drag-over"));
  dropZone.addEventListener("drop", async (event) => {
    event.preventDefault();
    dropZone.classList.remove("drag-over");
    if (opening || dropping) return;
    const files = [...event.dataTransfer.files];
    if (files.length !== 1) {
      message.textContent = "请一次拖入一个目录或文件。";
      return;
    }
    dropping = true;
    dropZone.setAttribute("aria-busy", "true");
    try {
      const path = desktopFiles.pathForFile(files[0]);
      if (!path) throw Error("无法读取路径，请从文件资源管理器拖入本地目录或文件。");
      const chosenDimension = dimension.value;
      // 先由 Go 识别来源种类、规范化路径，再走原有打开、转换与取消流程。
      if (!await browse(path) || !dialog.open) return;
      if ([...dimension.options].some((option) => option.value === chosenDimension)) dimension.value = chosenDimension;
      await choose(input.value);
    } catch (error) { message.textContent = String(error); }
    finally {
      dropping = false;
      dropZone.setAttribute("aria-busy", "false");
    }
  });
  document.querySelector("#browse").onclick = () => browse(input.value);
  input.addEventListener("input", () => { setDimensions([""]); });
  input.addEventListener("keydown", (event) => {
    if (event.key === "Enter") { event.preventDefault(); browse(input.value); }
  });
  document.querySelector("#parent").onclick = () => browse(listing.parent);
  document.querySelector("#open-directory").onclick = () => choose(input.value);
  return ({ kind: nextKind = "world", choose: callback = openWorld, world = {} } = {}) => {
    kind = nextKind;
    currentWorld = world;
    onChoose = callback;
    dimension.value = world.dimension || "";
    setDimensions(world.dimensions || [""]);
    document.querySelector("#source-fix").checked = world.bedrockFix ?? true;
    document.querySelector("#source-memory").value = world.structureMemoryMiB || 512;
    document.querySelector("#source-workers").value = world.structureWorkers || 0;
    document.querySelector("#source-options").hidden = kind === "pack";
    document.querySelector("#file-drop-title").textContent = kind === "pack"
      ? "将材质包 ZIP 或目录拖到这里使用"
      : "将存档目录或文件拖到这里打开";
    dialog.querySelector("h2").textContent = kind === "pack" ? "选择 Java 材质包" : "打开存档或结构";
    document.querySelector("#open-directory").textContent = kind === "pack" ? "使用材质包" : "打开所选来源";
    document.querySelector("#source-hint").textContent = kind === "pack"
      ? "选择 zip 或包含 assets 的目录，也可直接输入路径。"
      : "选择文件后点击打开；双击存档目录可浏览内部。结构文件转换到旁边的同名目录。";
    dialog.showModal();
    browse(world.path || paths[kind]);
  };
}
