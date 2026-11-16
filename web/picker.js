/** 文件选择器只使用服务端给出的完整路径，不在浏览器里拼接平台路径。 */
export function createPicker(openWorld) {
  const dialog = document.querySelector("#files"),
    input = document.querySelector("#directory"),
    list = document.querySelector("#file-list"),
    message = document.querySelector("#file-message");
  let listing, kind = "world", onChoose = openWorld;
  const paths = { world: "", pack: "" };
  async function browse(path = "") {
    message.textContent = "正在读取目录…";
    input.disabled = true;
    document.querySelector("#open-directory").disabled = true;
    document.querySelector("#parent").disabled = true;
    try {
      const response = await fetch(
        "/api/files?kind=" + kind + "&path=" + encodeURIComponent(path),
      );
      if (!response.ok) throw Error(await response.text());
      listing = await response.json();
      input.value = listing.path;
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
        const name = document.createElement("span");
        name.textContent = entry.name;
        const kind = document.createElement("span");
        kind.className = "kind";
        kind.textContent = entry.pack ? "材质包" : entry.world ? "Java 存档" : "目录";
        row.append(name, kind);
        row.onclick = () =>
          entry.pack || entry.world ? choose(entry.path) : browse(entry.path);
        list.append(row);
      }
      message.textContent = listing.entries.length + " 个项目";
    } catch (error) {
      message.textContent = String(error);
    } finally {
      input.disabled = false;
      document.querySelector("#open-directory").disabled = false;
      document.querySelector("#parent").disabled = !listing;
    }
  }
  async function choose(path) {
    message.textContent = kind === "pack" ? "正在合成材质包…" : "正在打开存档…";
    try {
      await onChoose(path);
      paths[kind] = path;
      dialog.close();
    } catch (error) {
      message.textContent = String(error);
    }
  }
  document.querySelector("#browse").onclick = () => browse(input.value);
  input.addEventListener("keydown", (event) => {
    if (event.key === "Enter") {
      event.preventDefault();
      browse(input.value);
    }
  });
  document.querySelector("#parent").onclick = () => browse(listing.parent);
  document.querySelector("#open-directory").onclick = () => choose(input.value);
  return ({ kind: nextKind = "world", choose: callback = openWorld } = {}) => {
    kind = nextKind;
    onChoose = callback;
    dialog.querySelector("h2").textContent = kind === "pack" ? "选择 Java 材质包" : "打开 Java 存档";
    document.querySelector("#open-directory").textContent = kind === "pack" ? "使用当前材质包目录" : "打开当前存档目录";
    dialog.querySelector(".hint").textContent = kind === "pack" ? "选择 zip 文件或包含 assets 的材质包目录，也可直接输入完整路径。" : "请选择包含 level.dat 和 region 的 Java 主世界目录。存档以只读方式打开。";
    dialog.showModal();
    browse(paths[kind]);
  };
}
