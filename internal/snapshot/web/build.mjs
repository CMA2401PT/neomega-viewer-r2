// 生成 Go 要嵌入的离线播放组件。模块内嵌着色器与 worker，导出的 HTML 在 file:// 下也能启动。
import { createRequire } from "node:module";
import { mkdirSync, readFileSync, readdirSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";


import { websrModels } from "../../../../../neomega-r2/r2-frontend/scripts/websr-models.mjs";

const here = dirname(fileURLToPath(import.meta.url));
const r2Root = fileURLToPath(new URL("../../../../../neomega-r2/", import.meta.url));
const coreDist = join(r2Root, "r2-core/dist");
const assetsDist = join(r2Root, "assets-utils/dist");
const frontendDist = join(r2Root, "r2-frontend/dist");
const require = createRequire(join(r2Root, "package.json"));
const { build } = require("esbuild");


const shaderFiles = readdirSync(join(coreDist, "render")).filter((name) => name.endsWith(".wgsl"));
const shaders = Object.fromEntries(shaderFiles.map((name) => [name, readFileSync(join(coreDist, "render", name), "utf8")]));
const sceneWorker = (await build({
  entryPoints: [join(coreDist, "scene/worker.js")],
  bundle: true,
  format: "iife",
  write: false,
  target: "es2022",
  minify: true,
})).outputFiles[0].text;

const main = (await build({
  entryPoints: [join(here, "main.ts")],
  write: false,
  bundle: true,
  format: "esm",
  target: "es2022",
  minify: true,
  define: {
    "import.meta.url": "document.baseURI",
    R2_SHADERS: JSON.stringify(shaders),
    R2_SCENE_WORKER: JSON.stringify(sceneWorker),
    R2_WEBSR_MODELS: JSON.stringify(websrModels),
  },
  plugins: [{
    name: "r2-workspace",
    setup(b) {
      b.onResolve({ filter: /^neomega-r2-core$/ }, () => ({ path: join(coreDist, "index.js") }));
      b.onResolve({ filter: /^neomega-r2-materials$/ }, () => ({ path: join(assetsDist, "index.js") }));
      b.onResolve({ filter: /^neomega-r2-frontend\/ui$/ }, () => ({ path: join(frontendDist, "ui.js") }));
    },
  }],
})).outputFiles[0].text;

const uiDir = join(r2Root, "r2-frontend/src/ui");
const styleCSS = readFileSync(join(uiDir, "style.css"), "utf8");
const actions = '<select id="export-dimension" aria-label="维度" hidden></select>';
const layout = readFileSync(join(uiDir, "layout.html"), "utf8")
  .replace('<slot name="actions"></slot>', actions)
  .replace(/<slot name="[^"]+"><\/slot>/g, "");

const player = `<style>${styleCSS}</style>
${layout}
<script type="application/json" id="r2-resources"><!-- R2_MATERIALS --></script>
<script type="module">${main}</script>`;
mkdirSync(join(here, "dist"), { recursive: true });
writeFileSync(join(here, "dist/player.html"), player);
