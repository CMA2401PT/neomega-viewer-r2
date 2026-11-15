/** 文件选择器只使用服务端给出的完整路径，不在浏览器里拼接平台路径。 */
export function createPicker(openWorld) {
  const dialog = document.querySelector("#files"),
    input = document.querySelector("#directory"),
    list = document.querySelector("#file-list"),
    message = document.querySelector("#file-message");
  let listing;
  async function browse(path = "") {
    message.textContent = "正在读取目录…";
    input.disabled = true;
    document.querySelector("#open-directory").disabled = true;
    document.querySelector("#parent").disabled = true;
    try {
      const response = await fetch(
        "/api/files?path=" + encodeURIComponent(path),
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
        kind.textContent = entry.world ? "Java 存档" : "目录";
        row.append(name, kind);
        row.onclick = () =>
          entry.world ? choose(entry.path) : browse(entry.path);
        list.append(row);
      }
      message.textContent = listing.entries.length + " 个目录";
    } catch (error) {
      message.textContent = String(error);
    } finally {
      input.disabled = false;
      document.querySelector("#open-directory").disabled = false;
      document.querySelector("#parent").disabled = !listing;
    }
  }
  async function choose(path) {
    message.textContent = "正在打开存档…";
    try {
      await openWorld(path);
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
  return () => {
    dialog.showModal();
    browse(input.value);
  };
}
