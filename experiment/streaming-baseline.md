# 固定版本流式应用实测（dec23c0）

本报告对应应用 `dec23c0`、核心 `3314923`。当时远景仍使用简化静态模型，以下加载与性能结果不适用于完整静态远景。复现请检出对应版本。

这里验证 Java 来源、区域流式更新和统一射线核心组合后的行为。浏览器脚本从空白欢迎页打开独立测试存档，再打开“新天堂”，检查移动、视距变化、快速跳转和加载中切换存档。性能脚本使用同一应用路径完成加载，随后测量固定视点。

2026-11-15 的结果来自 Apple M4、桌面 Chrome、ANGLE Metal，使用 headless 模式和像素比 1。默认近景半径 8 区块，远景半径 48 区块，即 1536×1536×512 方块。测试未清空操作系统文件缓存，未模拟远程网络；Windows 图形驱动上的运行效果仍需实机验证。

## 准备输入与运行

在工作区根目录创建一个独立的小型 Java 存档：

```sh
go run ./apps/neomega-viewer-r2/experiment/fixture -out /tmp/neomega-r2-java-fixture
go run ./apps/neomega-viewer-r2/cmd/viewer -listen 127.0.0.1:2413
```

测试存档含草地、附魔台和石英砖，不是欢迎场景。服务启动时不指定 `-world`，以便验证空白启动。另一个终端执行：

```sh
R2_WORLD='/Users/dai/Downloads/新天堂' node apps/neomega-viewer-r2/experiment/browser.cjs
R2_WORLD='/Users/dai/Downloads/新天堂' node apps/neomega-viewer-r2/experiment/performance.cjs
```

按上述顺序串行运行，避免 GPU 测试相互干扰。`R2_URL` 可替换服务地址，`R2_FIXTURE` 可替换测试存档路径；`PLAYWRIGHT_MODULE` 与 `CHROME` 指定本机工具路径。`R2_OUTPUT` 指定产物目录，两个脚本默认分别写入 `/tmp/viewer-r2-check` 与 `/tmp/viewer-r2-performance`。

## 区域加载与生命周期

`browser.cjs` 使用 1280×720 画布。以下数据来自 [streaming.json](results/streaming.json)，时间从打开“新天堂”开始；此时共享模型已由前面的独立存档加载完成。

| 检查 | 结果 |
|---|---|
| 首个区域发布 | 169 ms。 |
| 默认完整窗口 | 2894 ms，144 个区域，传输约 82.6 MiB。 |
| 从 X=1280 移动到 X=1440 | 新增 12 个区域请求，复用其他 132 个区域。 |
| 返回同一相机 | 画布逐像素一致。 |
| 远景半径从 48 缩到 24 | 已发布区域从 144 降到 36，区域池占用从约 164.7 MiB 降到 40.2 MiB。 |
| 林冠视点近景半径从 8 改到 16 | 精细 section 从 1086 增到 4590，没有新增区域下载。 |
| 跳至远处后立即返回 | 取消过时读取，最终画面逐像素一致。 |
| 加载过程中切换存档 | 只保留目标存档的 section 和活动对象，无旧区域残留。 |
| 独立 Java 附魔台 | 活动书通过真实来源接口加载，关闭对象绘制会改变画面。 |

这些时间包含浏览器实际流式发布，但不是网络冷启动基准。缩小视距回收的是池内地址；预留的 256 MiB 区域纹理在关闭存档时释放。空白欢迎页不下载模型资源。

## 原生 1080p 绘制成本

`performance.cjs` 使用 1920×1080 实际像素，每像素一条射线，FOV 为 65°。每个视点等待所有区域和近景对象准备完成，再重置 GPU 查询并取得 240 个样本，舍弃前 40 个。计时包含每帧实例变换上传与绘制，不包含此前的区域加载。原始样本和回调间隔见 [performance.json](results/performance.json)。

| 视点 | 相机 → 目标 | GPU 中位数 | GPU P95 |
|---|---|---:|---:|
| 高空 | 1280,271,1280 → 1120,185,1080 | 14.98 ms | 15.47 ms |
| 林冠 | 1280,78,1280 → 1210,70,1200 | 12.36 ms | 13.78 ms |
| 建筑 | 1180,85,1160 → 1110,72,1090 | 13.09 ms | 14.38 ms |

三个固定视点均在 16.67 ms 的 GPU 帧预算内，结果与固定窗口研究版的同档结果接近。这不保证所有移动、首次编译和显卡配置都达到 60 FPS。原图见 [高空](results/aerial-1080p.png)、[建筑](results/building-1080p.png) 和 [独立活动对象](results/objects.png)。

## 核心与 Go 检查

以下命令验证来源传输、跨区裁片、显存地址回收、实例句柄生命周期与依赖方向：

```sh
./check.sh neomega-r2 apps/neomega-viewer-r2
node --test neomega-r2/tests/*.mjs
```

HTTP 测试确认无效存档打开失败时保留当前会话、关闭或切换后旧请求返回 409。带 NBT 的测试处理器只在近景被调用，远景区域请求不会调用它。浏览器的五项核心测试包含负坐标跨区候选、替换本体和旧实例槽复用。

核心独立页面另外执行 `check.cjs` 和 `special.cjs`：五个静态场景的分层与逐格遍历最大通道差为 0，玻璃 8 格与 16 格厚度颜色相同；活动书页改变 6830 个像素，旗面透明孔与石英遮挡探针通过。结果见 [静态回归](../../../neomega-r2/experiment/results/refactor/static.json) 和 [对象回归](../../../neomega-r2/experiment/results/refactor/special.json)。特殊场景仍保留旧版已有的一个静态边界像素舍入差异，没有因通用实例接口增加差异。
