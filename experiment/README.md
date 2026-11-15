# 完整静态模型功能验收

静态方块在整个可见范围使用同一个共享模型编号。近景半径只控制 effector 和 block actor，不参与静态形状选择。这里以实际 Java 存档和 Chrome 绘制验证这个约定，不采集帧率或加载性能。

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

## 验证内容

使用 Apple M4、Chrome ANGLE Metal、1280×720 实际像素、单射线。相机为 `(128,6,168)`，目标为 `(128,2,128)`。近景半径为 1 时，目标静态方块位于近景范围外。

- 栏杆、楼梯和花保留真实形状。关闭特殊对象绘制后，近景半径 1 与 16 的静态像素完全一致。
- 改变近景半径可以启用、卸载附魔书，不改变静态本体。
- 关闭跨格裁片后画面确实改变，证明告示牌伸入邻格的部分由候选片段绘制。
- 将观察窗口移动到来源区域之外后，来源区域作为不可见邻接数据保留，目标区域仍收到片段。
- 移动返回后，静态画面逐像素一致；没有 WebGL 错误或页面异常。

结果见 [功能断言](results/static-models.json) 和 [远景静态模型原图](results/static-models-far.png)。Go 检查包含单份模型调色板传输、只在近景调用 NBT 处理器和会话切换；核心测试包含跨区片段、实例与显存地址回收。

旧版简化远景的性能和流式加载结果保留在 [固定版本报告](streaming-baseline.md)。它们不能用来评价完整静态远景；当前没有重新测量性能。
