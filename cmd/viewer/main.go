// viewer 组装文件来源、统一射线核心与流式浏览页面。
package main

import (
	"context"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"neomega-converter-core/structure"
	r2 "neomega-r2"
	"neomega-r2/blockactor"
	"neomega-r2/effector"
	app "neomega-viewer-r2"
	"neomega-viewer-r2/internal/desktop"
	"neomega-viewer-r2/internal/server"
	"neomega-viewer-r2/internal/worlddata"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

// version 由发布构建通过 -ldflags "-X main.version=v…" 注入，本地构建显示 dev。
var version = "dev"

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

// run 在资源准备完成且端口监听成功后显示地址，退出时等待正在处理的请求结束。
func run() error {
	listen := flag.String("listen", "127.0.0.1:2412", "监听地址")
	world := flag.String("world", "", "启动时打开 Java、Bedrock、PackFS 或结构文件，留空显示 Möbius")
	connect := flag.String("connect", "", "主动连接 R2Link 数据源（tcp:// 或 ws://）")
	connectMemory := flag.Int("connect-memory-mib", 512, "连接世界内存预算（MiB）")
	dimension := flag.String("dimension", "", "Java 维度目录或 Bedrock 维度 0/1/2，默认主世界")
	bedrockFix := flag.Bool("bedrock-fix", true, "修复 Bedrock 转换后的邻接属性")
	structureMemory := flag.Int("structure-memory-mib", 512, "结构转换工作内存预算（MiB，至少 64）")
	structureWorkers := flag.Int("structure-workers", 0, "结构写出并发上限，0 自动选择")
	webPath := flag.String("web", "", "开发时读取应用网页目录")
	browser := flag.Bool("browser", true, "启动后自动打开应用窗口")
	angle := flag.String("angle", "gl", "打开窗口时使用的 ANGLE 后端：gl、vulkan、d3d11；system 表示系统默认浏览器")
	showVersion := flag.Bool("version", false, "打印版本号后退出")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return nil
	}
	if *world != "" && *connect != "" {
		return fmt.Errorf("-world 与 -connect 请选择一个")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	colored := desktop.TerminalColors()
	desktop.PrintBanner(os.Stdout, version, colored)
	progress := desktop.Progress{Out: os.Stdout, Colored: colored}
	progress.Step("准备中…")
	progress.Step("读取模型目录…")
	catalog, err := r2.OpenCatalog()
	if err != nil {
		return err
	}
	progress.Step("建立方块状态映射…")
	mapping := worlddata.StateMapping(catalog)
	// 新的特效和 NBT 方块处理器在这里显式组装，核心不决定应用启用哪些内容。
	service := server.New(
		mapping,
		effector.Registry{"minecraft:enchanting_table": effector.Kind("enchanting_book")},
		blockactor.Registry{},
	)
	service.Options.SkipBedrockFix = !*bedrockFix
	service.Options.Structure = structure.Options{MemoryMiB: *structureMemory, Workers: *structureWorkers}
	service.ConnectMemoryMiB = *connectMemory
	defer service.Close()
	if *connect != "" {
		if _, err := service.Connect(server.ConnectOptions{
			URL:        *connect,
			MemoryMiB:  *connectMemory,
			BedrockFix: bedrockFix,
		}); err != nil {
			return err
		}
	}
	if *world != "" {
		path := filepath.Clean(*world)
		if filepath.Base(path) == "level.dat" {
			path = filepath.Dir(path)
		}
		progress.Step("打开存档 " + filepath.Base(path) + "…")
		if _, err = service.Open(ctx, server.OpenOptions{
			Path:      *world,
			Dimension: *dimension,
			Progress: func(value structure.Progress) {
				progress.Step(fmt.Sprintf("结构转换 %s：%d/%d", value.Stage, value.Completed, value.Total))
			},
		}); err != nil {
			return err
		}
	}
	web, err := fs.Sub(app.Web, "web")
	if err != nil {
		return err
	}
	if *webPath != "" {
		web = os.DirFS(*webPath)
	}
	listener, err := net.Listen("tcp", *listen)
	if err != nil {
		return err
	}
	httpServer := &http.Server{
		Handler:           service.Handler(web),
		ReadHeaderTimeout: 5 * time.Second,
	}
	done := make(chan error, 1)
	go func() { done <- httpServer.Serve(listener) }()
	progress.Done()
	address := desktop.BrowserAddress(listener.Addr())
	desktop.PrintReady(os.Stdout, address, colored)
	if *browser {
		// 开机/双击时没有任何参数，用这里的默认后端直接打开应用窗口。
		if *angle == "system" {
			desktop.OpenBrowser(address)
			fmt.Println("  窗口：系统默认浏览器")
		} else {
			fmt.Println("  窗口：" + desktop.LaunchApp(address, *angle))
		}
	}
	select {
	case err := <-done:
		if err != http.ErrServerClosed {
			return err
		}
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return httpServer.Shutdown(shutdown)
}
