// viewer 组装 Java 来源、统一射线核心与流式浏览页面。
package main

import (
	"flag"
	"fmt"
	"io/fs"
	r2 "neomega-r2"
	"neomega-r2/blockactor"
	"neomega-r2/effector"
	app "neomega-viewer-r2"
	"neomega-viewer-r2/internal/server"
	"net/http"
	"os"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:2412", "监听地址")
	world := flag.String("world", "", "启动时只读打开 Java 存档，留空显示空白欢迎页")
	webPath := flag.String("web", "", "开发时读取应用网页目录")
	flag.Parse()
	catalog, err := r2.OpenCatalog()
	if err != nil {
		panic(err)
	}
	// 新的特效和 NBT 方块处理器在这里显式组装，核心不决定应用启用哪些内容。
	service := server.New(catalog, effector.Registry{"minecraft:enchanting_table": effector.Kind("enchanting_book")}, blockactor.Registry{})
	defer service.Close()
	if *world != "" {
		if _, err = service.Open(*world); err != nil {
			panic(err)
		}
	}
	web, err := fs.Sub(app.Web, "web")
	if err != nil {
		panic(err)
	}
	if *webPath != "" {
		web = os.DirFS(*webPath)
	}
	fmt.Println("neOmega Viewer R2：http://" + *listen)
	if err = http.ListenAndServe(*listen, service.Handler(web)); err != nil {
		panic(err)
	}
}
