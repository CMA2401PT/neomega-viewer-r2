// 独立入口也能报告模块下载或初始化失败，避免只留下空白画布。
const loading = document.getElementById("loading");
loading.textContent = "正在初始化渲染器、编译着色器…";
try {
  await import("./app.js");
} catch (error) {
  console.error(error);
  loading.textContent = "渲染器启动失败";
  const notice = document.getElementById("notice");
  notice.textContent = String(error);
  notice.hidden = false;
}
