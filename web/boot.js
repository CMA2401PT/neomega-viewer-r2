// 独立入口也能报告模块下载或初始化失败，避免只留下空白画布。
import { startupStage, failStartup } from "./startup.js";
const loading = document.getElementById("loading");
try {
  await startupStage("正在加载渲染模块…");
  await import("./app.js");
} catch (error) {
  failStartup(error);
  console.error(error);
  loading.textContent = "渲染器启动失败";
  const notice = document.getElementById("notice");
  notice.textContent = String(error);
  notice.hidden = false;
}
