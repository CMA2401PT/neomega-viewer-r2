// java-source 监听 R2Link，把 Java 存档按客户端的关注范围发送出去。
// 在工作区根目录运行：go run ./apps/neomega-viewer-r2/examples/java-source -world /path/to/world
// R2 连接 tcp://127.0.0.1:2414；加 -transport ws 则连接 ws://127.0.0.1:2414/r2link。
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"celler"
	"celler/java"
	"github.com/coder/websocket"
	"neomega-converter-core/worldinput"
	"neomega-viewer-r2/r2link"
)

// main 同一个示例可监听 TCP 或 WS，文件句柄随每个连接独立关闭。
func main() {
	address := flag.String("listen", "127.0.0.1:2414", "数据源监听地址")
	transport := flag.String("transport", "tcp", "tcp 或 ws")
	path := flag.String("world", "", "Java 世界目录或 .pack")
	dimension := flag.String("dimension", "", "Java 维度目录")
	resetEvery := flag.Duration("camera-every", 30*time.Second, "定时位置重置间隔，0 关闭")
	flag.Parse()
	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()
	serve := func(conn *r2link.Conn) {
		defer conn.Close()
		stopClose := context.AfterFunc(ctx, func() { conn.Close() })
		defer stopClose()
		if err := sendWorld(
			ctx,
			conn,
			*path,
			*dimension,
			*resetEvery,
		); err != nil && ctx.Err() == nil {
			log.Print(err)
		}
	}
	if *transport == "ws" {
		mux := http.NewServeMux()
		mux.HandleFunc("/r2link", func(w http.ResponseWriter, r *http.Request) {
			conn, err := websocket.Accept(w, r, nil)
			if err != nil {
				return
			}
			serve(r2link.WebSocket(conn, 512<<20))
		})
		server := &http.Server{Addr: *address, Handler: mux}
		context.AfterFunc(ctx, func() { server.Close() })
		fmt.Printf("R2 数据源：ws://%s/r2link\n", *address)
		if err := server.ListenAndServe(); err != nil && ctx.Err() == nil {
			log.Fatal(err)
		}
	} else {
		listener, err := net.Listen("tcp", *address)
		if err != nil {
			log.Fatal(err)
		}
		defer listener.Close()
		context.AfterFunc(ctx, func() { listener.Close() })
		fmt.Printf("R2 数据源：tcp://%s\n", listener.Addr())
		for {
			conn, err := listener.Accept()
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				log.Fatal(err)
			}
			go serve(r2link.TCP(conn, 512<<20))
		}
	}
}

// incoming 只保留一条待处理的 R2 消息，发送大世界时也能及时收到新范围。
type incoming struct {
	message r2link.Message
	err     error
}

// sendWorld 先 welcome，再等 ready 和 interest，之后补发范围内的区块。
// 每条 chunk 的 replace 覆盖整根柱子；interest 矩形含两端，交集内容继续有效。
// 控制消息由独立协程读取，主循环串行写入，避免两条 TCP 消息交错。
func sendWorld(
	ctx context.Context,
	conn *r2link.Conn,
	path, dimension string,
	resetEvery time.Duration,
) error {
	hello, err := conn.Read(ctx)
	if err != nil {
		return err
	}
	if hello.Type != "hello" || hello.Version != 1 {
		return fmt.Errorf("需要 R2Link v1 hello")
	}
	source, err := worldinput.Open(
		ctx,
		worldinput.Options{Path: path, Dimension: dimension},
	)
	if err != nil {
		return err
	}
	defer source.Close()
	if source.Java == nil {
		return fmt.Errorf("java-source 使用 Java 世界或 PackFS")
	}
	positions, err := source.Java.Chunks(ctx)
	if err != nil {
		return err
	}
	camera := r2link.Camera{Position: source.Info.Position}
	welcome := r2link.Message{
		Type: "welcome", Protocol: "r2link", Version: 1, Blocks: "java", Delivery: "interest",
		Scene: &r2link.Scene{
			Name: source.Info.Name, Dimension: source.Info.Dimension,
			MinY: -64, Height: 512,
		},
		Camera: &camera,
	}
	if err := conn.Write(ctx, welcome); err != nil {
		return err
	}
	session, cancel := context.WithCancel(ctx)
	messages := make(chan incoming)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			message, err := conn.Read(session)
			select {
			case messages <- incoming{message, err}:
			case <-session.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()
	defer func() { cancel(); conn.Close(); <-done }()
	var timer <-chan time.Time
	if resetEvery > 0 {
		ticker := time.NewTicker(resetEvery)
		defer ticker.Stop()
		timer = ticker.C
	}
	sent := map[celler.ChunkPos]bool{}
	queue := []celler.ChunkPos{}
	ready := false
	// 非空队列启用 work 分支；空队列只等待消息和定时控制。
	available := make(chan struct{})
	close(available)
	for {
		var work <-chan struct{}
		if len(queue) > 0 {
			work = available
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case item := <-messages:
			if item.err != nil {
				return item.err
			}
			switch item.message.Type {
			case "ready":
				ready = true
				clear(sent)
				queue = nil
			case "interest":
				if !ready {
					continue
				}
				for pos := range sent {
					if !r2link.Contains(item.message.Ranges, pos.X, pos.Z) {
						delete(sent, pos)
					}
				}
				queue = nil
				for _, pos := range positions {
					if !sent[pos] && r2link.Contains(item.message.Ranges, pos.X, pos.Z) {
						queue = append(queue, pos)
					}
				}
			case "error":
				return fmt.Errorf("R2：%s", item.message.Message)
			}
		case <-timer:
			if ready {
				if err := conn.Write(
					ctx,
					r2link.Message{Type: "camera.reset", Position: &camera.Position},
				); err != nil {
					return err
				}
			}
		case <-work:
			pos := queue[0]
			queue = queue[1:]
			message, err := readChunk(ctx, source.Java, pos)
			if err != nil {
				return err
			}
			if err := conn.Write(ctx, message); err != nil {
				return err
			}
			sent[pos] = true
		}
	}
}

// readChunk 只发送方块状态，实体和模型编号都不进入协议。
func readChunk(
	ctx context.Context,
	reader *worldinput.JavaReader,
	pos celler.ChunkPos,
) (r2link.Message, error) {
	message := r2link.Message{Type: "chunk", Mode: "replace", X: pos.X, Z: pos.Z}
	records, err := reader.Read(ctx, pos)
	if err != nil || records[0] == nil {
		return message, err
	}
	raw, err := records[0].Decode()
	if err != nil {
		return message, err
	}
	chunk, err := java.DecodeChunk(raw)
	if err != nil {
		return message, err
	}
	ys, err := chunk.SectionYs()
	if err != nil {
		return message, err
	}
	for _, y := range ys {
		section, err := chunk.Section(y)
		if err != nil {
			return message, err
		}
		if section.Blocks != nil {
			message.Sections = append(
				message.Sections,
				r2link.Section{Y: y, Layers: []r2link.Layer{r2link.FromStorage(section.Blocks)}},
			)
		}
	}
	return message, nil
}
