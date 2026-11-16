// viewer 组装 Java 来源、统一射线核心与流式浏览页面。
package main

import (
	"context"
	"flag"
	"fmt"
	"io/fs"
	"log"
	r2 "neomega-r2"
	"neomega-r2/blockactor"
	"neomega-r2/effector"
	app "neomega-viewer-r2"
	"neomega-viewer-r2/internal/desktop"
	"neomega-viewer-r2/internal/javaworld"
	"neomega-viewer-r2/internal/server"
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
	world := flag.String("world", "", "启动时只读打开 Java 存档，留空显示 Möbius 默认场景")
	webPath := flag.String("web", "", "开发时读取应用网页目录")
	browser := flag.Bool("browser", true, "启动后用默认浏览器打开页面")
	showVersion := flag.Bool("version", false, "打印版本号后退出")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return nil
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
	mapping := javaworld.StateMapping(catalog)
	// 新的特效和 NBT 方块处理器在这里显式组装，核心不决定应用启用哪些内容。
	service := server.New(
		mapping,
		effector.Registry{"minecraft:enchanting_table": effector.Kind("enchanting_book")},
		blockactor.Registry{},
	)
	defer service.Close()
	if *world != "" {
		path := filepath.Clean(*world)
		if filepath.Base(path) == "level.dat" {
			path = filepath.Dir(path)
		}
		progress.Step("打开存档 " + filepath.Base(path) + "…")
		if _, err = service.Open(*world); err != nil {
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
		desktop.OpenBrowser(address)
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
