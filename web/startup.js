// 启动状态独立于运行中的区块加载提示，避免 HUD 切换世界时将它清空。
const panel = document.getElementById("startup-status");
const label = document.getElementById("startup-stage");
const elapsed = document.getElementById("startup-elapsed");
const started = performance.now();
let active = true;
const ticker = setInterval(() => {
  elapsed.textContent = `已等待 ${Math.floor((performance.now() - started) / 1000)} 秒`;
}, 1000);

/** 让状态先出现在屏幕上，再进入可能同步阻塞的显卡调用。 */
export function paintStartup() {
  return new Promise((resolve) => requestAnimationFrame(() => setTimeout(resolve, 0)));
}

/** @param {string} message */
export async function startupStage(message) {
  if (!active) return;
  label.textContent = message;
  await paintStartup();
}

/** 首批场景已经实际绘制，之后交给 HUD 显示持续加载状态。 */
export function finishStartup() {
  if (!active) return;
  active = false;
  clearInterval(ticker);
  panel.hidden = true;
}

/** @param {unknown} error */
export function failStartup(error) {
  active = false;
  clearInterval(ticker);
  panel.hidden = false;
  label.textContent = "启动失败";
  elapsed.textContent = String(error);
}
