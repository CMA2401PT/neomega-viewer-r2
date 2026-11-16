// link-source 为浏览器验收提供可控的 TCP/WS 数据源，控制接口只用于实验脚本。
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"net"
	"net/http"
	"sync"

	"github.com/coder/websocket"
	"neomega-viewer-r2/r2link"
)

// peer 把控制请求和 interest 自动补发串行写到同一个传输。
type peer struct {
	mu   sync.Mutex
	conn *r2link.Conn
}

// send 保护 TCP 消息边界，避免两条测试命令交错写入。
func (p *peer) send(message r2link.Message) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.conn.Write(context.Background(), message)
}

// fixture 用计数与最后范围让浏览器脚本观察真实握手和按需供给。
type fixture struct {
	mu          sync.Mutex
	blocks      string
	delivery    string
	peers       map[*peer]bool
	connections int
	ready       int
	ranges      []r2link.Range
	sent        int
}

// serve 每次连接都独立握手，自动供给仅用于 interest 的移动实验。
func (f *fixture) serve(conn *r2link.Conn) {
	p := &peer{conn: conn}
	defer conn.Close()
	f.mu.Lock()
	f.connections++
	blocks, delivery := f.blocks, f.delivery
	f.mu.Unlock()
	hello, err := conn.Read(context.Background())
	if err != nil || hello.Type != "hello" {
		return
	}
	dimension := "."
	if blocks == "bedrock" {
		dimension = "0"
	}
	if err := p.send(r2link.Message{Type: "welcome", Protocol: "r2link", Version: 1, Blocks: blocks, Delivery: delivery, Scene: &r2link.Scene{Name: "R2Link 验收", Dimension: dimension, MinY: -64, Height: 384}, Camera: &r2link.Camera{Position: [3]float64{8, 40, 24}}}); err != nil {
		return
	}
	f.mu.Lock()
	f.peers[p] = true
	f.mu.Unlock()
	defer func() { f.mu.Lock(); delete(f.peers, p); f.mu.Unlock() }()
	sent := map[[2]int32]bool{}
	for {
		message, err := conn.Read(context.Background())
		if err != nil {
			return
		}
		switch message.Type {
		case "ready":
			f.mu.Lock()
			f.ready++
			f.mu.Unlock()
			clear(sent)
		case "interest":
			f.mu.Lock()
			f.ranges = message.Ranges
			f.mu.Unlock()
			if delivery != "interest" {
				continue
			}
			for pos := range sent {
				if !r2link.Contains(message.Ranges, pos[0], pos[1]) {
					delete(sent, pos)
				}
			}
			for _, view := range message.Views {
				pos := [2]int32{int32(math.Floor(view.Position[0] / 16)), int32(math.Floor(view.Position[2] / 16))}
				if sent[pos] || !r2link.Contains(message.Ranges, pos[0], pos[1]) {
					continue
				}
				chunk := r2link.Message{Type: "chunk", Mode: "replace", X: pos[0], Z: pos[1], Sections: []r2link.Section{{Y: 0, Layers: []r2link.Layer{{Palette: []string{"minecraft:stone []"}}}}}}
				if err := p.send(chunk); err != nil {
					return
				}
				sent[pos] = true
				f.mu.Lock()
				f.sent++
				f.mu.Unlock()
			}
		}
	}
}

// main 同时开放 TCP、WS 与命令入口，实验脚本通过消息本身测试更新。
func main() {
	tcp := flag.String("tcp", "127.0.0.1:2426", "TCP 地址")
	httpAddress := flag.String("http", "127.0.0.1:2428", "WS 和控制接口地址")
	flag.Parse()
	f := &fixture{blocks: "java", delivery: "push", peers: map[*peer]bool{}}
	listener, err := net.Listen("tcp", *tcp)
	if err != nil {
		log.Fatal(err)
	}
	defer listener.Close()
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go f.serve(r2link.TCP(conn, 512<<20))
		}
	}()
	mux := http.NewServeMux()
	mux.HandleFunc("/r2link", func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err == nil {
			f.serve(r2link.WebSocket(conn, 512<<20))
		}
	})
	mux.HandleFunc("POST /configure", func(w http.ResponseWriter, r *http.Request) {
		var value struct {
			Blocks   string
			Delivery string
		}
		if err := json.NewDecoder(r.Body).Decode(&value); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		f.mu.Lock()
		f.blocks, f.delivery = value.Blocks, value.Delivery
		f.mu.Unlock()
	})
	mux.HandleFunc("POST /send", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		message, err := r2link.Decode(body)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		f.mu.Lock()
		peers := []*peer{}
		for p := range f.peers {
			peers = append(peers, p)
		}
		f.mu.Unlock()
		for _, p := range peers {
			if err := p.send(message); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
		}
	})
	mux.HandleFunc("POST /disconnect", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		for p := range f.peers {
			p.conn.Close()
		}
		f.mu.Unlock()
	})
	mux.HandleFunc("GET /stats", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		json.NewEncoder(w).Encode(map[string]any{"connections": f.connections, "ready": f.ready, "peers": len(f.peers), "ranges": f.ranges, "sent": f.sent})
	})
	fmt.Printf("TCP %s；WS ws://%s/r2link\n", *tcp, *httpAddress)
	log.Fatal(http.ListenAndServe(*httpAddress, mux))
}
