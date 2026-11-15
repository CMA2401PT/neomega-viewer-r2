# neomega-viewer-r2：Java 存档射线浏览器

这个应用把 Java 存档读取、流式调度和 R2 统一射线核心组合成可运行的浏览器。近景使用完整静态模型和已注册的特殊对象，远景使用简化模型；完整立方体在两者中都保留表面贴图。近远景共用绘制器，远景的数据处理仍然只做调色板映射。

默认启动为空白欢迎页，点击“打开存档”选择服务端机器上的 Java 存档目录。存档只读打开，目前支持主世界。初始相机来自 `level.dat` 中的出生点，并向上偏移 12 格；没有欢迎演示、固定测试场景或针对特定存档的处理。

## 启动与使用

在工作区根目录运行：

```sh
go run ./apps/neomega-viewer-r2/cmd/viewer -listen 127.0.0.1:2412
```

浏览器打开 <http://127.0.0.1:2412>。目录需包含 `level.dat` 和 `region`。也可启动时直接打开：

```sh
go run ./apps/neomega-viewer-r2/cmd/viewer \
  -listen 127.0.0.1:2412 -world '/path/to/java-world'
```

拖动转向，WASD 移动，E 或空格上升，Q 下降，Shift 加速。“坐标”可直接跳转；“关闭存档”卸载世界并回到空白页。设置保存在浏览器本地。

设置分别控制近景和远景：

- 近景默认为半径 8 区块，可调为 1～16。水平范围为圆形，垂直范围为上下 `min(半径+1,12)` 个 section。section 是 16×16×16 的存储块。
- 远景默认为半径 48 区块，可调为 16～48，步长 8。默认覆盖 1536×1536×512 方块，Y 为 -64..447。窗口按 128 方块对齐随相机移动，因此实际边界有对齐偏移。
- 渲染默认使用屏幕原生像素、每像素一条射线。分辨率与采样数独立设置，不会为了显示帧率自动降低画质。

进度栏分别显示近景准备和远景覆盖。“场景信息”提供非空近远景 section 数、区域数、累计传输、区域池用量、纹理分配和 GPU 时间。

## 为什么移动不必重新加载整个窗口

服务启动时建立一次“Java 状态 → 近远景模型编号”映射。区域接口读取和解压存档，仅转换调色板条目，保留压缩格子索引，不生成模型、剔面或合并网格。一个区域包含 8×8 区块；服务端保留最多约 64 MiB 的区域响应缓存。

浏览器分别限制网络下载、后台准备和 GPU 发布。四路请求优先读取靠近相机的区域；后台线程选择近远景模型并生成跳跃层次；主线程每帧发布一个完成的区域，未完成部分稍后补齐。移动时复用重叠区域，离开范围的区域撤销候选并回收地址。近景视距变化只重做受影响区域的外观选择，不重新下载已有索引。

来源、渲染与应用调度分为三个边界：

| 位置 | 责任 |
|---|---|
| `internal/javaworld/` | 只读 Java 主世界、压缩调色板传输；独立的近景对象读取入口。 |
| `internal/server/` | 存档会话、区域缓存、请求并发与 HTTP。切换存档后拒绝旧会话请求。 |
| `internal/filebrowse/` | 服务端文件浏览与 Java 目录识别。 |
| `web/stream.js` | 可见范围、下载取消、近远景更新、逐区发布与卸载。 |
| `web/app.js`、`controls.js`、`picker.js` | 页面组装、飞行相机与文件选择，不含射线算法。 |
| `neomega-r2/web/` | 共享模型、纯数据场景准备、GPU 目录与统一求交。 |

本应用不依赖旧 viewer 或 viewer-core。核心的 Go 代码也不反向依赖存档库。远景与近景使用同一份来源索引，但 NBT 外观与特效仅由近景对象接口处理。

## 特殊对象的扩展位置

Go 入口 `cmd/viewer/main.go` 选择需要启用的 effector 和 block actor 处理器。`internal/javaworld.World.Objects` 仅扫描近景中被处理器认领的方块，将状态、位置和 NBT 交给它们。浏览器入口 `web/app.js` 注册对应类型的外观工厂。工厂返回部件、贴图、包围盒和动画，公共实例层负责格子候选与射线绘制。

附魔台的活动书已通过这条链路接入。block actor 的处理器和传输接口已连通，并通过携带 NBT 的测试处理器验证；旗帜图案、告示牌排版等具体实现尚未添加。详细契约和新增实现的方法见 [核心说明](../../neomega-r2/README.md#怎样添加-effector-和-block-actor)。含水方块暂只显示本体，不显示附加水层。

区域池预留 256 MiB，缩小视距回收池内地址，关闭存档释放整个池。异常密集存档超出容量时会显示错误，可降低远景视距。当前流式更新指相机移动、视距改变和存档切换，不监听磁盘上正在被游戏写入的内容；查看外部修改需重新打开存档。

## 构建与验收

网页和共享资源嵌入 Go 二进制，没有前端打包步骤。开发时可用 `-web ./apps/neomega-viewer-r2/web` 直接读取应用网页；核心网页仍取自嵌入资源，改核心后需重新编译。

```sh
./check.sh neomega-r2 apps/neomega-viewer-r2
node --test neomega-r2/tests/*.mjs
go run ./apps/neomega-viewer-r2/experiment/fixture -out /tmp/neomega-r2-java-fixture
R2_WORLD='/path/to/新天堂' node apps/neomega-viewer-r2/experiment/browser.cjs
```

浏览器验收需先在 2413 启动一个未打开存档的应用服务。脚本会打开测试目录和指定存档，改变视距并切换世界。独立测试存档含附魔台和石英砖，不参与应用欢迎页。工具路径、输出与结果解释见 [验收说明](experiment/README.md)。

不含调试符号的发布构建：

```sh
mkdir -p apps/neomega-viewer-r2/dist
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags='-s -w' \
  -o apps/neomega-viewer-r2/dist/neomega-viewer-r2-windows-amd64.exe ./apps/neomega-viewer-r2/cmd/viewer
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags='-s -w' \
  -o apps/neomega-viewer-r2/dist/neomega-viewer-r2-macos-arm64 ./apps/neomega-viewer-r2/cmd/viewer
```
