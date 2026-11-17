package server

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"neomega-viewer-r2/r2link"
)

// 连续推送与 HTTP 构建同时进行，关闭必须先停接收任务再释放会话。
func TestLinkCommitAndCloseConcurrent(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var peers sync.WaitGroup
	acceptDone := make(chan struct{})
	go func() {
		defer close(acceptDone)
		for {
			socket, err := listener.Accept()
			if err != nil {
				return
			}
			peers.Add(1)
			go func() {
				defer peers.Done()
				conn := r2link.TCP(socket, 1<<20)
				defer conn.Close()
				stop := context.AfterFunc(ctx, func() { conn.Close() })
				defer stop()
				if _, err := conn.Read(ctx); err != nil {
					return
				}
				if err := conn.Write(ctx, r2link.Message{Type: "welcome", Protocol: "r2link", Version: 1, Blocks: "java", Delivery: "push", Scene: &r2link.Scene{Name: "race", Dimension: ".", Height: 384}}); err != nil {
					return
				}
				if _, err := conn.Read(ctx); err != nil {
					return
				}
				for i := 0; ; i++ {
					message := r2link.Message{Type: "chunk", Mode: "replace", Sections: []r2link.Section{{Y: int32(i % 2), Layers: []r2link.Layer{{Palette: []string{"minecraft:stone []"}}}}}}
					if err := conn.Write(ctx, message); err != nil {
						return
					}
				}
			}()
		}
	}()
	service := New(nil, nil)
	defer service.Close()
	handler := service.Handler(fstest.MapFS{})
	var readers sync.WaitGroup
	reading, stopReading := context.WithCancel(ctx)
	for range 3 {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for reading.Err() == nil {
				id := service.Info().ID
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, httptest.NewRequest("GET", fmt.Sprintf("/api/world/region?world=%d&x=0&z=0", id), nil))
				if response.Code != 200 && response.Code != 409 {
					t.Errorf("读取响应：%d %s", response.Code, response.Body.String())
					return
				}
			}
		}()
	}
	for range 5 {
		if _, err := service.Connect(ConnectOptions{URL: "tcp://" + listener.Addr().String(), MemoryMiB: 1}); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(5 * time.Second)
		for {
			info := service.Info()
			if info.LinkStats != nil && info.LinkStats.Chunks > 0 {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("连接没有提交区块")
			}
			time.Sleep(time.Millisecond)
		}
		service.Close()
		if info := service.Info(); info.Name != "Möbius" || info.Connection != nil {
			t.Fatal(info)
		}
	}
	stopReading()
	readers.Wait()
	cancel()
	listener.Close()
	<-acceptDone
	peers.Wait()
}

// 页面只随 SSE 登记，断开后必须自动注销，并取消它贡献的关注范围。
func TestSSEPageLifetime(t *testing.T) {
	service := New(nil, nil)
	defer service.Close()
	server := httptest.NewServer(service.Handler(fstest.MapFS{}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	request, err := http.NewRequestWithContext(ctx, "GET", server.URL+"/api/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	scanner := bufio.NewScanner(response.Body)
	if !scanner.Scan() {
		t.Fatal(scanner.Err())
	}
	var initial event
	if err := json.Unmarshal([]byte(strings.TrimPrefix(scanner.Text(), "data: ")), &initial); err != nil {
		t.Fatal(err)
	}
	if initial.Type != "sync" || initial.Page == 0 {
		t.Fatal(initial)
	}
	body := fmt.Sprintf(`{"page":%d,"world":%d,"ranges":[{"minX":-2,"minZ":-2,"maxX":2,"maxZ":2}]}`, initial.Page, initial.World.ID)
	posted, err := http.Post(server.URL+"/api/world/view", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, posted.Body)
	posted.Body.Close()
	if posted.StatusCode != 204 {
		t.Fatal(posted.StatusCode)
	}
	cancel()
	response.Body.Close()
	deadline := time.Now().Add(time.Second)
	for {
		service.mu.RLock()
		count := len(service.pages)
		service.mu.RUnlock()
		if count == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("SSE 断开后页面仍存在")
		}
		time.Sleep(time.Millisecond)
	}
}
