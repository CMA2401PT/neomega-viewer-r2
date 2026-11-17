// viewer 组装文件来源、统一射线核心与流式浏览页面。
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
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
	"runtime"
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
	desktopHost := flag.Bool("desktop-host", false, "桌面宿主模式：stdout 上报 JSON 就绪消息，stdin 关闭时退出，不启动外部浏览器")
	open := flag.String("open", desktop.Choices[0].Key, "启动时打开窗口用的浏览器与图形后端：edge-vulkan、chrome-vulkan、chrome-gl；system 表示系统默认浏览器")
	showVersion := flag.Bool("version", false, "打印版本号后退出")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return nil
	}
	if *world != "" && *connect != "" {
		return fmt.Errorf("-world 与 -connect 请选择一个")
	}
	if _, ok := desktop.FindChoice(*open); !ok && *open != "system" {
		return fmt.Errorf("-open 不认识 %q", *open)
	}
	// 诊断日志放在 exe 旁边；测试者遇到问题时把整份文件发回来。
	diag := desktop.OpenDiagnostics("neomega-viewer-r2-log.txt")
	defer diag.Close()
	diag.Environment(version)
	log.SetOutput(io.MultiWriter(os.Stderr, diag))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if *desktopHost {
		// 宿主独占 stdin 管道；正常关闭或宿主崩溃都通过 EOF 释放服务。
		go func() {
			_, _ = io.Copy(io.Discard, os.Stdin)
			stop()
		}()
	}
	colored := desktop.TerminalColors()
	var output io.Writer = os.Stdout
	if *desktopHost {
		output = io.Discard
	}
	desktop.PrintBanner(output, version, colored)
	progress := desktop.Progress{Out: output, Colored: colored}
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
	handler := http.NewServeMux()
	handler.Handle("POST /api/diagnostics", diag.Handler())
	handler.Handle("/", service.Handler(web))
	httpServer := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}
	done := make(chan error, 1)
	go func() { done <- httpServer.Serve(listener) }()
	progress.Done()
	address := desktop.BrowserAddress(listener.Addr())
	desktop.PrintReady(output, address, colored)
	diag.Printf("服务就绪：%s", address)
	if *desktopHost {
		if err := json.NewEncoder(os.Stdout).Encode(struct {
			Event string `json:"event"`
			URL   string `json:"url"`
			Log   string `json:"log"`
		}{Event: "ready", URL: address, Log: diag.Path}); err != nil {
			_ = httpServer.Close()
			return err
		}
	} else {
		// 程序位于沙箱之类带特殊权限的目录时，从这里启动的浏览器实测必定打不开或白屏（换浏览器、换配置目录都没用），
		// 所以不自动打开、也不显示按钮，只明确告诉用户把程序挪到别处。
		restricted, sddl := desktop.RestrictedLocation()
		if restricted != "" {
			diag.Printf("程序所在位置带特殊权限，不打开浏览器：%s %s", restricted, sddl)
			lines := []string{
				"无法在这里打开浏览器：程序所在目录 " + restricted + " 带有沙箱之类的特殊权限（例如开发工具保护的工作区），",
				"从这里启动的浏览器会打不开或一直白屏。请把程序复制到别的目录（比如桌面）再运行。",
			}
			for _, line := range lines {
				if colored {
					line = "\x1b[1;93m" + line + "\x1b[0m"
				}
				fmt.Println("  " + line)
			}
		} else {
			if *browser {
				openWindow(address, *open, diag)
			}
			if runtime.GOOS == "windows" {
				// 终端里列出几种浏览器与图形后端组合，点按钮或按数字键用另一种组合再开一个窗口。
				restore, interactive := desktop.ListenChoices(func(i int) {
					if name, err := desktop.LaunchChoice(address, i, diag); err != nil {
						fmt.Printf("  打不开 %s：%v\n", desktop.Choices[i].Name, err)
					} else {
						fmt.Println("  窗口：" + name)
					}
				})
				defer restore()
				desktop.PrintChoices(os.Stdout, colored, interactive)
			}
		}
		if diag.Path != "" {
			fmt.Println("  诊断日志：" + diag.Path + "（遇到问题时把这个文件发给开发者）")
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

// openWindow 按 -open 打开第一个窗口；指定组合的浏览器没装时按 Choices 的顺序尝试下一种，都没有就用系统默认浏览器。
func openWindow(address, key string, diag *desktop.Diagnostics) {
	if key == "system" {
		desktop.OpenBrowser(address)
		diag.Printf("用系统默认浏览器打开")
		fmt.Println("  窗口：系统默认浏览器")
		return
	}
	first, _ := desktop.FindChoice(key)
	for n := range desktop.Choices {
		i := (first + n) % len(desktop.Choices)
		if name, err := desktop.LaunchChoice(address, i, diag); err == nil {
			fmt.Println("  窗口：" + name)
			return
		}
	}
	desktop.OpenBrowser(address)
	diag.Printf("没有找到 Edge 或 Chrome，用系统默认浏览器打开")
	fmt.Println("  窗口：系统默认浏览器（没有找到 Edge 或 Chrome，无法选择图形后端）")
}
