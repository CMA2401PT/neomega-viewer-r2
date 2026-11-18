// 把启动与渲染的关键事件交给本机服务写进诊断日志（exe 旁的 neomega-viewer-r2-log.txt）。
// 不同电脑的显卡、驱动和浏览器后端差别很大，测试者把日志贴回来就能看到卡在哪一步。发送失败不影响页面。

export function report(event: string, detail: Record<string, unknown> = {}) {
  try {
    fetch("/api/diagnostics", {
      method: "POST",
      keepalive: true,
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ event, ms: Math.round(performance.now()), ...detail }),
    }).catch(() => {});
  } catch {}
}

/** 页面环境：浏览器、屏幕、显卡与图形后端。用一个临时 WebGL2 上下文读显卡名，读完立即释放。 */
function environment(useWebGPU: boolean) {
  const info: Record<string, unknown> = {
    userAgent: navigator.userAgent,
    devicePixelRatio,
    window: [innerWidth, innerHeight],
    screen: [screen.width, screen.height],
    webgpu: "gpu" in navigator,
  };
  if (useWebGPU) return { ...info, backend: "webgpu" };
  try {
    const gl = document.createElement("canvas").getContext("webgl2");
    if (!gl) return { ...info, webgl2: false };
    const debug = gl.getExtension("WEBGL_debug_renderer_info");
    Object.assign(info, {
      webgl2: true,
      vendor: debug ? gl.getParameter(debug.UNMASKED_VENDOR_WEBGL) : gl.getParameter(gl.VENDOR),
      renderer: debug ? gl.getParameter(debug.UNMASKED_RENDERER_WEBGL) : gl.getParameter(gl.RENDERER),
      version: gl.getParameter(gl.VERSION),
      parallelCompile: !!gl.getExtension("KHR_parallel_shader_compile"),
      floatBlend: !!gl.getExtension("EXT_float_blend"),
      timer: !!gl.getExtension("EXT_disjoint_timer_query_webgl2"),
      maxTexture: gl.getParameter(gl.MAX_TEXTURE_SIZE),
      max3DTexture: gl.getParameter(gl.MAX_3D_TEXTURE_SIZE),
    });
    gl.getExtension("WEBGL_lose_context")?.loseContext();
  } catch (error) {
    info.probeError = String(error);
  }
  return info;
}

export const describe = (error: unknown) =>
  error instanceof Error ? `${error.message}\n${error.stack || ""}`.slice(0, 4000) : String(error).slice(0, 4000);

/** 页面打开时上报环境，并记录所有未处理的错误。 */
export function startDiagnostics(useWebGPU: boolean) {
  report("page", environment(useWebGPU));
  addEventListener("error", (event) =>
    report("error", { message: describe(event.error ?? event.message), source: `${event.filename}:${event.lineno}` }),
  );
  addEventListener("unhandledrejection", (event) => report("rejection", { message: describe(event.reason) }));
}
