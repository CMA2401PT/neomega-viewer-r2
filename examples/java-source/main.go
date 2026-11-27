// java-source 是最小的 R2Link 数据源：把一个 Java 存档按浏览器的订阅逐区域发送。
// 在工作区根目录运行：go run ./apps/neomega-viewer-r2/examples/java-source -world /path/to/world
// 再在查看器的连接框填写 ws://127.0.0.1:2414/r2link。
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"neomega-viewer-r2/internal/worldsource"
	"neomega-viewer-r2/resources"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"neomega-converter-core/worldinput"
	r2link "neomega-r2link"
)

func main() {
	address := flag.String("listen", "127.0.0.1:2414", "数据源监听地址")
	path := flag.String("world", "", "Java 世界目录或 .pack")
	dimension := flag.String("dimension", "", "Java 维度目录")
	cameraEvery := flag.Duration("camera-every", 0, "每隔多久把相机移回出生点，0 关闭")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	release, err := resources.Open()
	if err != nil {
		log.Fatal(err)
	}
	opener := worldsource.Opener{Tables: release.Tables}
	source, err := opener.Open(ctx, worldsource.Options{Options: worldinput.Options{Path: *path, Dimension: *dimension}, SkipBedrockFix: true})
	if err != nil {
		log.Fatal(err)
	}
	defer source.Close()
	if source.Input.Java == nil {
		log.Fatal("java-source 使用 Java 世界或 PackFS")
	}
	spawn := r2link.Camera{Position: source.Input.Info.Position}
	options := r2link.Options{Scene: r2link.Scene{Name: source.Input.Info.Name}, Camera: &spawn}

	mux := http.NewServeMux()
	mux.HandleFunc("/r2link", func(w http.ResponseWriter, r *http.Request) {
		// nil：接受任何页面来源，查看器可以从别的地址连接过来。
		conn, err := r2link.Accept(w, r, nil)
		if err != nil {
			return
		}
		session := r2link.NewSession(conn, source, options)
		if *cameraEvery > 0 {
			go func() {
				ticker := time.NewTicker(*cameraEvery)
				defer ticker.Stop()
				for {
					select {
					case <-r.Context().Done():
						return
					case <-ticker.C:
						if session.MoveCamera(r.Context(), spawn) != nil {
							return
						}
					}
				}
			}()
		}
		if err := session.Run(r.Context()); err != nil && ctx.Err() == nil {
			log.Print(err)
		}
	})
	server := &http.Server{Addr: *address, Handler: mux}
	context.AfterFunc(ctx, func() { server.Close() })
	fmt.Printf("R2Link 数据源：ws://%s/r2link\n", *address)
	if err := server.ListenAndServe(); err != nil && ctx.Err() == nil {
		log.Fatal(err)
	}
}
