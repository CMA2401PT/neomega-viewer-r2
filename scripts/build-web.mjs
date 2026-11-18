// Viewer 与渲染核心使用同一个 TypeScript 编译器，先生成引擎和共享 UI 的类型声明。
import { execFileSync } from "node:child_process";
import { fileURLToPath } from "node:url";

const core = new URL("../../../neomega-r2/r2/", import.meta.url);
const web = fileURLToPath(new URL("../web", import.meta.url));
for (const args of [
  [fileURLToPath(new URL("scripts/build.mjs", core))],
  [fileURLToPath(new URL("node_modules/typescript/bin/tsc", core)), "-p", web],
  [fileURLToPath(new URL("node_modules/typescript/bin/tsc", core)), "-p", fileURLToPath(new URL("../cmd/exporter/web/", import.meta.url))],
  [fileURLToPath(new URL("../cmd/exporter/web/build.mjs", import.meta.url))],
]) {
  execFileSync(process.execPath, args, { stdio: "inherit" });
}
