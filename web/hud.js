// HUD 负责坐标编辑、面板锚定、时间按钮与临时提示；不持有渲染器或存档请求。
/** @typedef {Object} ViewerSettings
 * @property {number} nearRadius
 * @property {number} farRadius
 * @property {string} resolution
 * @property {number} speed
 * @property {boolean} shadows
 * @property {number} time
 * @property {boolean} timeFlow
 * @property {boolean} fog
 * @property {number} stillSamples
 * @property {number} photoScale
 * @property {number} photoSamples
 * @property {string} texturePath
 * @property {number} textureSize
 */
/** 页面元素由固定 HTML 提供，各界面模块使用同一套 ID。 @param {string} id */
export const element = (id) => document.getElementById(id);

/** 收起原生弹出面板，避免切换视图后留下顶层 UI。 */
export function closePopovers() {
  for (const panel of document.querySelectorAll(".popover:popover-open"))
    panel.hidePopover();
}

/** 让界面事件通过回调交给宿主，读数只在用户没有编辑时跟随相机。
 * @param {{settings:ViewerSettings,jump:(position:number[])=>void,changed:()=>void}} options
 */
export function createHUD({ settings, jump, changed }) {
  const fields = ["x", "y", "z"].map(element),
    form = element("teleport");
  let loaded = false,
    editing = false,
    toastTimer,
    loadingTimer,
    wasLoading = false;
  /** 放弃编辑时恢复实时坐标，跳转按钮不再接收旧值。 */
  function stopEditing() {
    editing = false;
    element("teleport-go").disabled = true;
  }
  form.onfocusin = (event) => {
    editing = true;
    element("teleport-go").disabled = !loaded;
    event.target.select?.();
  };
  form.onfocusout = (event) => {
    if (!form.contains(event.relatedTarget)) stopEditing();
  };
  form.onkeydown = (event) => {
    if (event.key === "Escape") document.activeElement.blur();
  };
  element("teleport-go").onmousedown = (event) => event.preventDefault();
  form.onsubmit = (event) => {
    event.preventDefault();
    jump(fields.map((field) => Number(field.value)));
    document.activeElement.blur();
    stopEditing();
  };
  /** 面板位于触发按钮上方，屏幕边缘向内收。
   * @param {HTMLElement} panel
   */
  function place(panel) {
    const trigger = document.querySelector(`[popovertarget="${panel.id}"]`);
    const rect = trigger.getBoundingClientRect();
    panel.style.left = `${Math.max(8, Math.min(innerWidth - panel.offsetWidth - 8, rect.left + rect.width / 2 - panel.offsetWidth / 2))}px`;
    panel.style.bottom = `${innerHeight - element("hud").getBoundingClientRect().top + 8}px`;
    panel.dataset.placed = "";
  }
  for (const panel of document.querySelectorAll(".popover")) {
    panel.addEventListener("toggle", (event) => {
      if (event.newState === "open") place(panel);
      else delete panel.dataset.placed;
    });
  }
  window.addEventListener("resize", () => {
    for (const panel of document.querySelectorAll(".popover:popover-open"))
      place(panel);
  });
  for (const [id, key] of [
    ["near-radius", "nearRadius"],
    ["far-radius", "farRadius"],
    ["resolution", "resolution"],
    ["speed", "speed"],
    ["fog", "fog"],
    ["still-samples", "stillSamples"],
    ["photo-scale", "photoScale"],
    ["photo-samples", "photoSamples"],
    ["time", "time"],
  ]) {
    const input = element(id),
      toggle = input.type === "checkbox";
    if (toggle) input.checked = settings[key];
    else input.value = String(settings[key]);
    input.addEventListener(id === "time" ? "input" : "change", () => {
      settings[key] = toggle
        ? input.checked
        : key === "resolution"
          ? input.value
          : Number(input.value);
      changed();
    });
  }
  for (const [id, key] of [
    ["shadows", "shadows"],
    ["time-flow", "timeFlow"],
  ])
    element(id).onclick = () => {
      settings[key] = !settings[key];
      changed();
    };
  element("resolution").options[0].textContent =
    `屏幕原生（${+devicePixelRatio.toFixed(2)}×）`;
  element("eye").onclick = () => {
    const hidden = document.body.classList.toggle("hud-hidden");
    element("eye").setAttribute("aria-pressed", String(hidden));
    element("eye").title = element("eye").ariaLabel = hidden
      ? "显示 HUD"
      : "隐藏 HUD";
    closePopovers();
  };
  return {
    /** 设置读数与按下态来自同一份设置，时间流动时也可刷新。 */
    settings() {
      element("near-value").textContent = `${settings.nearRadius} 区块`;
      element("far-value").textContent = `${settings.farRadius} 区块`;
      for (const [id, key] of [
        ["shadows", "shadows"],
        ["time-flow", "timeFlow"],
      ])
        element(id).setAttribute("aria-pressed", String(settings[key]));
      element("time-row").hidden = !settings.shadows;
      // 面板不显示时刻文字，悬停滑块时仍能看到具体时间。
      const minutes = Math.floor(settings.time * 60) % 1440;
      element("time").title =
        `${String(Math.floor(minutes / 60)).padStart(2, "0")}:${String(minutes % 60).padStart(2, "0")}`;
      element("time").value = String(settings.time);
    },
    /** 存档切换只影响依赖存档的操作。
     * @param {{loaded:boolean,name?:string,path?:string}} world
     */
    world(world) {
      loaded = world.loaded;
      element("world-name").textContent = world.loaded
        ? world.name
        : "未打开存档";
      element("info-name").textContent = world.loaded
        ? world.name
        : "未打开存档";
      element("info-path").textContent =
        world.path || "内置海晶灯莫比乌斯环；从文件夹按钮打开 Java 存档。";
      for (const id of ["map-open", "close-world", "x", "y", "z"])
        element(id).disabled = !loaded;
      stopEditing();
      clearTimeout(loadingTimer);
      wasLoading = false;
      element("loading").textContent = "";
      element("details").replaceChildren();
    },
    /** 坐标字段按数字长度展开，不为较大的世界坐标预留固定空白。
     * @param {number[]} position
     */
    position(position) {
      if (editing) return;
      fields.forEach((field, i) => {
        field.value = position[i].toFixed(1);
        field.style.width = `${Math.max(3, field.value.length) + 0.8}ch`;
      });
    },
    /** 只在工作待完成时显示进度，完成提示停留后淡出。
     * @param {{pending:number,nearPending:number,loaded:number,wanted:number}} stats
     */
    loading(stats) {
      const busy = stats.pending > 0 || stats.nearPending > 0;
      if (busy) {
        clearTimeout(loadingTimer);
        element("loading").classList.remove("faded");
        element("loading").textContent =
          `近景对象准备 ${stats.nearPending} 区域 · 远景覆盖 ${stats.loaded}/${stats.wanted}`;
      } else if (wasLoading) {
        element("loading").textContent = "已加载";
        loadingTimer = setTimeout(
          () => element("loading").classList.add("faded"),
          1500,
        );
      }
      wasLoading = busy;
    },
    /** 错误保持可读的短时提示，后来的错误替换先前内容。
     * @param {string} message
     */
    error(message) {
      clearTimeout(toastTimer);
      element("notice").textContent = message;
      element("notice").hidden = false;
      toastTimer = setTimeout(() => (element("notice").hidden = true), 5000);
    },
    /** 场景统计使用配对的标签和值，长路径留在单独的段落。
     * @param {[string,string][]} rows
     */
    details(rows) {
      element("details").replaceChildren(
        ...rows.map(([name, value]) => {
          const row = document.createElement("div"),
            dt = document.createElement("dt"),
            dd = document.createElement("dd");
          dt.textContent = name;
          dd.textContent = value;
          row.append(dt, dd);
          return row;
        }),
      );
    },
  };
}
