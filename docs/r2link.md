# R2Link：向 R2 推送方块世界

> **正在重写。** R2 的 Go 服务已不再连接数据源，TCP 传输已移除。新版本由浏览器直接通过 WebSocket 连接数据源，作为核心世界会话的一种数据源；协议会随之按会话的区域订阅调整。下文描述旧版本的消息格式，仅供参考；Python 示例仍是旧的 TCP 版本，暂时无法使用。

R2Link 是游戏机器人、服务器插件或存档服务向 R2 浏览器应用提供方块内容的协议。**数据源监听端口，R2 的 Go 服务主动连接数据源**；网页连接 R2，不直接连接数据源。一次会话按 `hello → welcome → ready → interest → chunk…` 建立：R2 先声明协议版本，数据源描述场景，R2 准备好内存世界后通知就绪，并持续告知观察范围。数据源可以更新方块、移动相机或清空场景。断线时 R2 保留最后画面，每 3 秒重试；重新连接从握手开始建立空世界。

方块内容使用游戏状态字符串，而不是 R2 的模型编号。Java 状态直接交给渲染适配器；Bedrock 状态经转换器变成 Java 状态，可选修复栅栏连接、楼梯拐角等邻接属性。所有来源共用 R2 的区域编码和静态模型目录。

## 消息边界

每条消息是一个 WebSocket Binary Message：

```text
消息体 = uint32LE(JSON 字节数) + UTF-8 JSON + 二进制索引
```

`LE` 表示小端。长度不包含自身四字节，按 UTF-8 字节数计算。WebSocket 使用完整消息边界，底层如何分片由 WebSocket 实现处理。控制消息的二进制尾部为空。

JSON 的 `type` 决定消息种类。方块调色板留在 JSON 中，4096 格的索引写进二进制尾部，兼顾可读性与体积。实现只需 JSON、小端整数读写及位运算。WebSocket 地址支持 `ws://` 和 `wss://`。需要认证时以后再加。

## 建立会话

R2 连接后发送：

```json
{"type":"hello","protocol":"r2link","version":1}
```

数据源回复：

```json
{
  "type":"welcome",
  "protocol":"r2link",
  "version":1,
  "blocks":"bedrock",
  "delivery":"interest",
  "scene":{"name":"机器人所见世界","dimension":"0","minY":-64,"height":384},
  "camera":{"position":[8,90,8],"yaw":0,"pitch":-0.3}
}
```

`blocks` 选择 `java` 或 `bedrock`，整个连接使用同一种命名。`delivery` 选择下文的 `interest` 或 `push`。这两个字段在重新连接时可以改变。

`scene` 描述一个维度。`name` 显示在 HUD，`dimension` 在 Bedrock 下是字符串 `"0"`、`"1"`、`"2"`，依次为主世界、下界、末地；Java 可以使用数据源自己的维度标识。`minY` 是维度起始方块高度，`height` 是正的高度跨度。R2 保留这两个字段，但渲染窗口固定为 `[-64,448)`。超出窗口的 section 保存在来源中并计入 HUD 诊断，显示时裁掉。

`camera` 可省略，默认位置为 `[0,minY+height/2,0]`。大世界宜提供有内容的初始位置。角度规则见“相机和场景控制”。

R2 完成来源准备后发送：

```json
{"type":"ready"}
```

紧接着发送当前 `interest`。`push` 来源收到 `ready` 就可以发送方块；`interest` 来源等到范围到达再供给。尚无网页观察时，范围为空。

## 方块状态字符串

调色板每项是一段 IR 状态文本，规则为：

```text
命名空间:方块名 ["属性名"=值,"另一个属性"=值]
```

名称与 `[` 之间有一个空格，无属性写 `[]`。属性名使用 JSON 字符串；Java 属性值也使用 JSON 字符串。Bedrock 属性值使用 JSON 字符串、十进制 int32 或 `true` / `false`。字符串里的引号和反斜杠按 JSON 规则转义，整段状态进入消息头时再作为 JSON 字符串转义一次。

```text
Java:    minecraft:stone []
Java:    minecraft:oak_log ["axis"="y"]
Bedrock: minecraft:oak_stairs ["upside_down_bit"=false,"weirdo_direction"=0]
Bedrock: minecraft:water ["liquid_depth"=0]
```

属性顺序可以不同，R2 会归一化。有方向、颜色等属性时应发送完整实际状态。Java 状态按哈希交给渲染器的状态表；Bedrock 通过共用转换表兼容名称与属性。未知语义按空气显示，Bedrock 转换缺失在场景信息中按当前保留内容统计。

R2Link v1 的内容是方块状态与层。NBT 方块实体、普通实体没有线格式；依赖 NBT 的文字和外观不能仅由状态恢复。Bedrock 第二层水会合并为 Java 的含水属性，额外水层的可见效果取决于 R2 的状态模型。

## 区块与 section 更新

一个 **section** 是 16×16×16 方块。一个 **区块**是固定 X、Z 下的所有 section。一条 `chunk` 消息更新一个区块，多个 section 在 R2 中一起提交。X、Z 是区块坐标，section 的 Y 是 section 坐标，例如 `x=-1,z=2,y=4` 的方块起点为 `(-16,64,32)`。

```json
{
  "type":"chunk",
  "x":-1,
  "z":2,
  "mode":"patch",
  "deleteSections":[],
  "sections":[{
    "y":4,
    "layers":[{
      "palette":["minecraft:air []","minecraft:stone []"],
      "bits":1
    }]
  }]
}
```

这条消息后面还要带该层的二进制索引。

| `mode` | 提交含义 |
|---|---|
| `replace` | 整块替换，未列出的 section 清除。`sections:[]` 保留一个已知空区块。 |
| `patch` | 保留其他 section，删除 `deleteSections` 指定的高度，再替换 `sections` 列出的高度。区块不存在时以空气开始。 |
| `delete` | 整块删除，地图占用表也移除该区块。 |

`sections`、`deleteSections` 缺省按空列表处理；X、Z 缺省为 0。首次供给或重新进入范围时使用 `replace` 最清晰，可以明确表达完整快照。实时数据只有部分 section 时也可直接发 `patch`。

Java section 恰好一层。Bedrock section 有一层或两层，第一层是本体，第二层是附加水层。每次 section 替换其全部层，省略第二层就清除旧的第二层。纯空气层可用 `palette:["minecraft:air []"],bits:0`。

### 二进制索引

尾部按 `sections` 顺序，再按 `layers` 顺序连接压缩字。长度由调色板和 `bits` 推导。

1. 局部格子坐标为 0～15，序号 `i=(x<<8)|(z<<4)|y`，Y 最快。
2. 调色板有 `N` 项，范围为 1～4096。`bits=ceil(log2(N))`；只有一项时 `bits=0`，不写压缩字。
3. `bits>0` 时，每个 uint32 放 `floor(32/bits)` 个索引，低位在前。一个索引不能跨字。
4. 字数为 `ceil(4096/floor(32/bits))`，以小端顺序写出。未使用的高位填零。

两项调色板的 `bits=1`，共 128 个 uint32，也就是 512 字节。第一格使用状态 1、其余使用状态 0 时，第一个字节为 `01`，其余 511 字节全为零。这些尺寸直接由上面的公式得到。

## 相机和场景控制

位置重置使用方块坐标，可以带小数：

```json
{"type":"camera.reset","position":[120.5,86,-31.5],"yaw":1.2,"pitch":-0.25}
```

角度单位是弧度。`yaw=0` 朝 -Z，正 yaw 转向 +X；`pitch=0` 水平，正 pitch 向上。省略 yaw 或 pitch 就保留页面相应朝向。R2 会结束照片采样和持续移动输入，收起地图，再把命令应用到所有页面。新打开或 SSE 重连的页面会同步最近一次位置命令。

清空当前内容：

```json
{"type":"scene.reset"}
```

同时改场景说明或移动相机：

```json
{
  "type":"scene.reset",
  "scene":{"name":"下界探索","dimension":"1","minY":0,"height":128},
  "camera":{"position":[8,80,8]}
}
```

`scene` 可以只带非空 `name` 或 `dimension`；高度变化一起提供 `minY` 和 `height`。省略的说明沿用原值。`camera` 省略时，已打开页面保留各自视角。

发送 `scene.reset` 后停止发送区块，等待新的 `ready` 和 `interest`，再发送新场景内容。R2 会清空方块、近景对象、地图占用表和区域缓存，并更换网页会话编号。传输连接继续使用，`blocks` 和 `delivery` 保持不变。

## 大世界的关注范围

`interest` 表达所有页面当前需要的区块矩形并集。每次消息完整替换上次范围，矩形的两端都包含：

```json
{
  "type":"interest",
  "views":[{"position":[8,90,8],"yaw":0,"pitch":-0.3,"nearRadius":2,"farRadius":32}],
  "ranges":[{"minX":-66,"minZ":-66,"maxX":65,"maxZ":65}]
}
```

`views` 提供相机和 UI 视距，便于决定发送优先级。**供给范围以 `ranges` 为准**，它包含模型跨区域片段、地图远景、主相机近景，以及启用 Bedrock 修复时所需的邻接区块。示例数值只说明格式。`views`、`ranges` 省略时按空列表处理。

- `interest` 模式适合可反复读取的大世界。R2 清除范围外区块，并直接丢弃范围外到达的数据。范围内区块无论何时到达都按正常提交处理。数据源补发新进入范围的区块；离开再回来时应重新提供完整快照。
- `push` 模式适合机器人陆续获得的现场数据。R2 保存收到的内容，直到数据源显式删除或重置。数据源可以使用 interest 决定发送优先级。

没有网页时，interest 范围为空，按需来源的保留区块会清除。每个页面随 SSE 连接登记，连接断开即移除。相机或范围变化合并到约 200 ms 的发送节奏。相机 reset 后，网页计算新范围并上报。

数据源不需要保存范围版本号。最简单的实现收到新范围就重发其中的完整快照；静态世界可以记录已发送区块，只补进入范围的部分。示例 Go 数据源采用后一种方式。

TCP/WS 的写入提供流量背压。一条消息限制在一个区块，发送循环应同时读取新的 interest，并在未发送队列里优先处理最新范围及控制指令。

## 断线与资源预算

断线时 R2 保留最后成功提交的画面，HUD 显示重试状态。每隔 3 秒重连一次，每次都发送新的 hello；数据源应把它当成新客户端会话。握手成功后，旧内存世界由新空世界替代。用户点击 HUD 的关闭来源按钮停止重试并返回 Möbius。

R2 默认给连接世界 512 MiB 预算，统计调色板、压缩索引和修复快照。一次提交超出预算时保留上次结果并断开，HUD 显示原因。单条在途消息也受这个预算限制。数据源可用 interest 供给，或删除已经不需要的 push 区块。R2 用户可以调整 `-connect-memory-mib` 或连接面板里的预算。

R2 和数据源可发送 `{"type":"error","message":"原因"}` 说明错误，随后结束连接。解码失败直接结束当前连接。

## 运行最小示例

在工作区根目录打开一个终端启动 R2：

```sh
go run ./apps/neomega-viewer-r2/cmd/viewer \
  -listen 127.0.0.1:2412 -browser=false
```

另一个终端运行只依赖 Python 标准库的小场景：

```sh
python3 apps/neomega-viewer-r2/examples/python-source/source.py --port 2415
```

打开网页 <http://127.0.0.1:2412>，点击 HUD 的链接图标（“连接数据源”），输入 `tcp://127.0.0.1:2415`。也可以启动 R2 时加 `-connect tcp://127.0.0.1:2415`。[Python 示例](../examples/python-source/source.py) 展示分帧、握手、两位调色板索引和一个完整区块。

需要提供真实 Java 世界时，运行 [Go 数据源](../examples/java-source/main.go)：

```sh
go run ./apps/neomega-viewer-r2/examples/java-source \
  -world '/path/to/java-world' -listen 127.0.0.1:2414
```

R2 连接 `tcp://127.0.0.1:2414`。`-world` 也可使用 Java `.pack`，`-dimension DIM-1` 选择下界。示例默认每 30 秒发送一次位置重置，`-camera-every 0` 关闭定时重置。

同一示例使用 WebSocket：

```sh
go run ./apps/neomega-viewer-r2/examples/java-source \
  -world '/path/to/java-world' -listen 127.0.0.1:2414 -transport ws
```

R2 连接 `ws://127.0.0.1:2414/r2link`。Go 实现可以复用应用公开包 `neomega-viewer-r2/r2link`；其中 `Encode/Decode` 处理消息体，`TCP/WebSocket` 包装已连接传输，`FromStorage` 将内部调色板改成协议位宽。WebSocket 帧处理使用 [coder/websocket](https://pkg.go.dev/github.com/coder/websocket)，不影响其他语言的数据源实现。
