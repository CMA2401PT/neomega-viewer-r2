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
./check.sh neomega-r2 apps/neomega-viewer-r2
node --test neomega-r2/tests/*.mjs
```

`R2_URL` 替换服务地址，`R2_FIXTURE` 替换验证存档，`R2_OUTPUT` 替换输出目录。浏览器工具路径可用 `PLAYWRIGHT_MODULE` 和 `CHROME` 指定。默认结果写入 `/tmp/r2-static-models`。

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

## 同设置 GPU 对照

不同阴影、视距和质量档位的 GPU 时间不能直接比较。`compare-renderer.cjs` 只替换浏览器收到的 `ray.frag` 和 `renderer.js`，其余资源、服务和来源保持相同。旧文件通过 `git show` 读取，工作区文件不用切换。

脚本使用“新天堂”的高空、树冠、建筑三个固定视角。设置为 1920×1080 原生分辨率、near 2、far 32、阴影和雾开启、游戏时间 10 点、水面时刻 0 秒。连续绘制预览帧，关闭累积与动态时间。每个视角收集 240 个 GPU 查询，去掉前 40 个再计算中位数和 P95；计时包含射线绘制与显示合成。脚本断言预览样本数为零，并保存参数、GPU、时间样本、界面截图及纯画布图片。

```sh
R2_URL=http://127.0.0.1:2417 \
R2_COMPARE=current,d964c8c,a412797,d964c8c,current \
R2_OUTPUT=/tmp/r2link/ui2/final-performance \
node apps/neomega-viewer-r2/experiment/compare-renderer.cjs
```

`current` 指工作区的绘制器，其他值是 `neomega-r2` 的 Git 版本。`R2_WORLD` 可替换存档路径，但三个取景点应一起调整。`R2_VIEWS=aerial` 可以只测高空视角。所有 GPU 测量顺序运行，避免另一浏览器的绘制竞争 GPU。

`R2_ISOLATE_DRAW=1` 在区域加载完成后暂停应用帧循环，直接按相同参数提交预览帧，并同步等待每帧 GPU 完成。这个对照把绘制程序成本与页面调度分开；GPU 查询仍只覆盖射线和显示合成。

需要核对新增应用调度的影响时，在 `R2_COMPARE` 中加入 `old-app`，使用工作区绘制器与应用 `766fe97` 的 `app.js`、`stream.js`，仍读取同一服务提供的 Java 区域。`R2_OLD_APP=1` 可让整组版本都使用旧应用调度。`R2_VARIANTS` 可指定存放临时着色器对照的目录，每个版本名下包含两份绘制文件。

## 静态模型验证内容

使用 Apple M4、Chrome ANGLE Metal、1280×720 实际像素、单射线。相机为 `(128,6,168)`，目标为 `(128,2,128)`。近景半径为 1 时，目标静态方块位于近景范围外。

- 栏杆、楼梯和花保留真实形状。关闭特殊对象绘制后，近景半径 1 与 16 的静态像素完全一致。
- 改变近景半径可以启用、卸载附魔书，不改变静态本体。
- 关闭跨格裁片后画面确实改变，证明告示牌伸入邻格的部分由候选片段绘制。
- 将观察窗口移动到来源区域之外后，来源区域作为不可见邻接数据保留，目标区域仍收到片段。
- 移动返回后，静态画面逐像素一致；没有 WebGL 错误或页面异常。

结果见 [功能断言](results/static-models.json) 和 [远景静态模型原图](results/static-models-far.png)。Go 检查包含单份模型调色板传输、只在近景调用 NBT 处理器和会话切换；核心测试包含跨区片段、实例与显存地址回收。

旧版简化远景的性能和流式加载结果保留在 [固定版本报告](streaming-baseline.md)。完整静态模型的绘制耗时使用上面的同设置对照脚本测量。
