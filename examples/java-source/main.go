// java-source 是最小的 R2Link 数据源：把一个 Java 存档按浏览器的订阅逐区域发送。
// 在工作区根目录运行：go run ./apps/neomega-viewer-r2/examples/java-source -world /path/to/world
// 再在查看器的连接框填写 ws://127.0.0.1:2414/r2link。
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"celler"
	"celler/java"
	"celler/layer_storage"
	"neomega-converter-core/worldinput"
	"neomega-r2link"
)

func main() {
	address := flag.String("listen", "127.0.0.1:2414", "数据源监听地址")
	path := flag.String("world", "", "Java 世界目录或 .pack")
	dimension := flag.String("dimension", "", "Java 维度目录")
	cameraEvery := flag.Duration("camera-every", 0, "每隔多久把相机移回出生点，0 关闭")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	input, err := worldinput.Open(ctx, worldinput.Options{Path: *path, Dimension: *dimension})
	if err != nil {
		log.Fatal(err)
	}
	defer input.Close()
	if input.Java == nil {
		log.Fatal("java-source 使用 Java 世界或 PackFS")
	}
	source := &javaSource{reader: input.Java}
	spawn := r2link.Camera{Position: input.Info.Position}
	options := r2link.Options{Scene: r2link.Scene{Name: input.Info.Name}, Camera: &spawn}

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

// javaSource 实现 r2link.Source 与 r2link.ChunkSource；不提供近景对象。
type javaSource struct {
	reader *worldinput.JavaReader
}

// Region 读取区域内的 8×8 个区块，把每个 section 的方块状态换成哈希后打包。
// 查看器只显示 Y -64～447，范围外的 section 计入 Clipped。
func (s *javaSource) Region(ctx context.Context, rx, rz int) (r2link.Region, error) {
	var region r2link.Region
	for x := rx * 8; x < rx*8+8; x++ {
		for z := rz * 8; z < rz*8+8; z++ {
			chunk, err := s.read(ctx, celler.ChunkPos{X: int32(x), Z: int32(z)})
			if err != nil || chunk == nil {
				if err != nil {
					return region, err
				}
				continue
			}
			ys, err := chunk.SectionYs()
			if err != nil {
				return region, err
			}
			for _, y := range ys {
				section, err := chunk.Section(y)
				if err != nil {
					return region, err
				}
				if section.Blocks == nil {
					continue
				}
				if y < -4 || y >= 28 {
					region.Clipped++
					continue
				}
				var blocks [4096]uint32
				air := true
				for i := range blocks {
					blocks[i] = r2link.BlockHash(section.Blocks.Get(layer_storage.At(i>>8, i&15, (i>>4)&15)))
					air = air && r2link.IsAir(blocks[i])
				}
				if !air {
					region.Sections = append(region.Sections, r2link.Pack([3]int{x, int(y), z}, &blocks))
				}
			}
		}
	}
	return region, ctx.Err()
}

func (s *javaSource) read(ctx context.Context, pos celler.ChunkPos) (*java.Chunk, error) {
	records, err := s.reader.Read(ctx, pos)
	if err != nil || records[0] == nil {
		return nil, err
	}
	raw, err := records[0].Decode()
	if err != nil {
		return nil, err
	}
	return java.DecodeChunk(raw)
}

// Chunks 给地图提供区块占用表。
func (s *javaSource) Chunks(ctx context.Context) (r2link.ChunkIndex, error) {
	positions, err := s.reader.Chunks(ctx)
	if err != nil {
		return nil, err
	}
	index := r2link.ChunkIndex{}
	for _, pos := range positions {
		index.Add(int(pos.X), int(pos.Z))
	}
	return index, nil
}
