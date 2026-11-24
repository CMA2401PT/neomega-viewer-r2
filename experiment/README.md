# R2 浏览器验收与性能对照

静态方块在整个可见范围使用同一个共享模型编号。近景半径只控制 effector 和 block actor，不参与静态形状选择。这里用实际存档和 Chrome 验证绘制、来源和界面，性能对照则单独固定相机与绘制参数。

`static-models.cjs` 打开独立验证存档，在固定相机下比较近景半径 1 与 16 的画面。输入含栏杆、楼梯、花、跨区域告示牌以及附魔台。告示牌只检查预生成的静态形状，不依赖 NBT 文字处理器。

## 运行

在工作区根目录创建测试存档并启动服务：

```sh
go run ./apps/neomega-viewer-r2/experiment/fixture -out /tmp/neomega-r2-static-fixture -static
go run ./apps/neomega-viewer-r2/cmd/viewer -listen 127.0.0.1:2416
```

另一个终端执行：

```sh
node apps/neomega-viewer-r2/experiment/static-models.cjs
go test ./neomega-r2/r2/... ./apps/neomega-viewer-r2/...
node --test neomega-r2/r2/tests/*.mjs
```

`R2_URL` 替换服务地址，`R2_FIXTURE` 替换验证存档，`R2_OUTPUT` 替换输出目录。浏览器工具路径可用 `PLAYWRIGHT_MODULE` 和 `CHROME` 指定。默认结果写入 `/tmp/r2-static-models`。

### 共享界面验收

`ui.cjs` 检查两个视口的面板、相机拾取、拍照保存与放弃，以及地图在区域到达后重试选点。可复用上面的测试存档和服务。设置 `R2_WORLD` 可以另选“world”截图使用的存档，拾取断言仍使用已知测试方块。

```sh
R2_URL=http://127.0.0.1:2416 R2_FIXTURE=/tmp/neomega-r2-static-fixture node apps/neomega-viewer-r2/experiment/ui.cjs
```

## 存档来源验收

`sources.cjs` 用真实 Java、Bedrock、Java 结构和 mcstructure 验证打开面板、选项传递、转换进度、输出复用及实际绘制。结构和 Bedrock 样本复制到输出目录下的独立临时目录，转换产物与副本放在一起。脚本记录输入路径、请求响应、渲染状态和截图，默认输出到 `/tmp/r2link/sources/`。

```sh
R2_URL=http://127.0.0.1:2413 \
R2_JAVA='/path/to/java-world' \
R2_BEDROCK='/path/to/bedrock-world' \
R2_STRUCTURE='/path/to/build.nbt' \
R2_MCSTRUCTURE='/path/to/build.mcstructure' \
node apps/neomega-viewer-r2/experiment/sources.cjs
```

未指定样本时使用工作区 `resource` 中的 Bedrock、天使翅膀 NBT 和全方块 mcstructure，以及 `~/Downloads/新天堂` Java 世界。各浏览器验收脚本顺序运行，因为它们共用服务端当前会话。

## WebGPU 版本性能对照

比较 v0.1.2 与 v0.1.4 的光影预览和静止累积时，使用专门的 WebGPU 计时脚本。版本配对、跨机器运行命令、GPU 计时范围和 M4 参考数据见[跨机器复测说明](webgpu-timing.md)。

## 怎样检查暗部颗粒是否还在收敛

静止累积与拍照都从第一个独立样本开始。每个样本的权重为 1，取满 N 个样本后，每个样本对该像素线性亮度的贡献都是 1/N。曝光、色调映射和所选的输出降噪在求平均之后进行。测量采样噪声时，把输出降噪设为「关闭」。

暗处主要靠反弹光照亮。同一块平面上，相邻像素各自抽取光线方向，有些方向看到亮处，有些被遮挡。因此同样的样本数，在阴影里仍容易看见相对亮度起伏。检查具体场景时，固定镜头、光照、曝光和分辨率，在同一条砖纹的亮部与暗部分别取一块区域，避开半影和纹理边缘。比较同一采样序列不同轮次的相同像素，可将固定砖纹与逐渐衰减的采样噪声区分开。用后续一段不重叠样本的平均亮度作为参考时，应说明参考本身也有采样误差。

## WebGL 同设置 GPU 对照

不同阴影、视距和质量档位的 GPU 时间不能直接比较。`compare-renderer.cjs` 只替换浏览器从 `/r2/` 收到的渲染核心（`neomega-r2/r2` 的 `dist`），应用页面、资源、服务和来源保持相同。旧版本在临时 git worktree 中用工作区的 TypeScript 构建，工作区文件不用切换。

脚本使用“新天堂”的高空、树冠、建筑三个固定视角。设置为 1920×1080 原生分辨率、near 2、far 32、阴影和雾开启、游戏时间 10 点、水面时刻 0 秒。连续绘制预览帧，关闭累积与动态时间。每个视角收集 240 个 GPU 查询，去掉前 40 个再计算中位数和 P95；计时包含射线绘制与显示合成。脚本断言预览样本数为零，并保存参数、GPU、时间样本、界面截图及纯画布图片。

```sh
R2_URL=http://127.0.0.1:2417 \
R2_COMPARE=current,320cbb2,current \
R2_OUTPUT=/tmp/r2link/ui2/final-performance \
node apps/neomega-viewer-r2/experiment/compare-renderer.cjs
```

`current` 指工作区已构建的 `dist`（先运行 `npm run build`），其他值是 `neomega-r2` 的 Git 版本。`R2_WORLD` 可替换存档路径，但三个取景点应一起调整。`R2_VIEWS=aerial` 可以只测高空视角。所有 GPU 测量顺序运行，避免另一浏览器的绘制竞争 GPU。

`R2_ISOLATE_DRAW=1` 在区域加载完成后暂停应用帧循环，直接按相同参数提交预览帧，并同步等待每帧 GPU 完成。这个对照把绘制程序成本与页面调度分开；GPU 查询仍只覆盖射线和显示合成。

旧版本必须与当前应用的绘制接口一致，即四档绘制器（`320cbb2`）之后的版本。`R2_VARIANTS` 可指定存放临时对照的目录，每个版本名下是一份完整的 `dist`，优先于同名的 Git 版本。

## 静态模型验证内容

使用 Apple M4、Chrome ANGLE Metal、1280×720 实际像素、单射线。相机为 `(128,6,168)`，目标为 `(128,2,128)`。近景半径为 1 时，目标静态方块位于近景范围外。

- 栏杆、楼梯和花保留真实形状。关闭特殊对象绘制后，近景半径 1 与 16 的静态像素完全一致。
- 改变近景半径可以启用、卸载附魔书，不改变静态本体。
- 关闭跨格裁片后画面确实改变，证明告示牌伸入邻格的部分由候选片段绘制。
- 将观察窗口移动到来源区域之外后，来源区域作为不可见邻接数据保留，目标区域仍收到片段。
- 移动返回后，静态画面逐像素一致；没有 WebGL 错误或页面异常。

结果见 [功能断言](results/static-models.json) 和 [远景静态模型原图](results/static-models-far.png)。Go 检查包含单份模型调色板传输、只在近景调用 NBT 处理器和会话切换；核心测试包含跨区片段、实例与显存地址回收。

绘制耗时使用上面的同设置对照脚本测量。
