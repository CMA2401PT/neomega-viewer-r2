package server

import (
	"container/list"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"neomega-converter-core/worldinput"
	"neomega-viewer-r2/internal/linksource"
	"neomega-viewer-r2/r2link"
)

// ConnectOptions 把地址与资源预算留在应用层，协议只描述来源内容。
type ConnectOptions struct {
	URL        string `json:"url"`
	BedrockFix *bool  `json:"bedrockFix,omitempty"`
	MemoryMiB  int    `json:"memoryMiB"`
}

// Connection 断线后仍描述最后的画面，状态单独更新而不清空来源。
type Connection struct {
	URL       string `json:"url"`
	State     string `json:"state"`
	Message   string `json:"message,omitempty"`
	MemoryMiB int    `json:"memoryMiB"`
	Delivery  string `json:"delivery,omitempty"`
}

// Connect 启动可取消的重连循环，握手成功前保留当前场景。
func (s *Server) Connect(options ConnectOptions) (Info, error) {
	parsed, err := url.Parse(options.URL)
	if err != nil {
		return Info{}, err
	}
	if parsed.Scheme != "tcp" && parsed.Scheme != "ws" && parsed.Scheme != "wss" {
		return Info{}, fmt.Errorf("连接地址使用 tcp://、ws:// 或 wss://")
	}
	if options.MemoryMiB == 0 {
		options.MemoryMiB = s.ConnectMemoryMiB
	}
	if options.MemoryMiB < 1 {
		return Info{}, fmt.Errorf("连接内存预算应为正数")
	}
	s.openMu.Lock()
	defer s.openMu.Unlock()
	s.stopLink()
	ctx, cancel := context.WithCancel(context.Background())
	s.linkCancel = cancel
	s.linkDone = make(chan struct{})
	s.mu.Lock()
	s.connection = &Connection{URL: options.URL, State: "connecting", MemoryMiB: options.MemoryMiB}
	s.publishConnectionLocked()
	s.mu.Unlock()
	go func() { defer close(s.linkDone); s.runLink(ctx, options) }()
	return s.Info(), nil
}

// stopLink 等循环退出时不持有内容锁，正在提交的接收任务才能结束。
func (s *Server) stopLink() {
	if s.linkCancel != nil {
		s.linkCancel()
		<-s.linkDone
		s.linkCancel = nil
	}
}

// runLink 每次断线等待固定间隔，从 hello 开始建立新的空世界。
func (s *Server) runLink(ctx context.Context, options ConnectOptions) {
	for {
		err := s.linkSession(ctx, options)
		if ctx.Err() != nil {
			return
		}
		s.mu.Lock()
		s.connection.State = "reconnecting"
		s.connection.Message = err.Error()
		s.publishConnectionLocked()
		s.mu.Unlock()
		timer := time.NewTimer(3 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

// received 让传输读取与关注范围发送共享一个有序提交循环。
type received struct {
	message r2link.Message
	err     error
}

// linkSession 在提交循环中处理数据和控制，读协程只持有一条待处理消息。
func (s *Server) linkSession(ctx context.Context, options ConnectOptions) error {
	conn, err := r2link.Dial(ctx, options.URL, int64(options.MemoryMiB)<<20)
	if err != nil {
		return err
	}
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	defer conn.Close()
	if err := conn.Write(
		ctx,
		r2link.Message{Type: "hello", Protocol: "r2link", Version: 1},
	); err != nil {
		return err
	}
	welcome, err := conn.Read(ctx)
	if err != nil {
		return err
	}
	if welcome.Type != "welcome" || welcome.Protocol != "r2link" || welcome.Version != 1 {
		return fmt.Errorf("需要 R2Link v1 welcome")
	}
	fix := !s.Options.SkipBedrockFix
	if options.BedrockFix != nil {
		fix = *options.BedrockFix
	}
	world, err := s.linkWorld(
		ctx,
		welcome,
		options,
		fix,
		false,
	)
	if err != nil {
		return err
	}
	sessionCtx, cancel := context.WithCancel(ctx)
	messages := make(chan received)
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		for {
			message, err := conn.Read(sessionCtx)
			select {
			case messages <- received{message, err}:
			case <-sessionCtx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()
	defer func() { cancel(); conn.Close(); <-readDone }()
	var ranges []r2link.Range
	dirty := map[[2]int]bool{}
	flush := func() {
		if len(dirty) == 0 {
			return
		}
		s.mu.Lock()
		keys := make([][2]int, 0, len(dirty))
		for key := range dirty {
			keys = append(keys, key)
		}
		info := s.infoLocked()
		s.broadcastLocked(event{
			Type: "regions", WorldID: info.ID,
			Version: s.current.version, Regions: keys, World: &info,
		})
		s.mu.Unlock()
		clear(dirty)
	}
	interest := func() error {
		s.mu.Lock()
		message := r2link.Message{Type: "interest"}
		for _, page := range s.pages {
			if page.world != s.current.info.ID {
				continue
			}
			message.Views = append(message.Views, page.view)
			for _, r := range page.ranges {
				if fix && welcome.Blocks == "bedrock" {
					r.MinX -= 2
					r.MinZ -= 2
					r.MaxX += 2
					r.MaxZ += 2
				}
				message.Ranges = append(message.Ranges, r)
			}
		}
		ranges = message.Ranges
		if welcome.Delivery == "interest" {
			keys, err := world.Retain(ranges)
			if err != nil {
				s.mu.Unlock()
				return err
			}
			s.invalidateLocked(keys)
			for _, key := range keys {
				dirty[key] = true
			}
		}
		s.mu.Unlock()
		return conn.Write(ctx, message)
	}
	ready := func() error {
		if err := conn.Write(ctx, r2link.Message{Type: "ready"}); err != nil {
			return err
		}
		return interest()
	}
	if err := ready(); err != nil {
		return err
	}
	notify := time.NewTicker(50 * time.Millisecond)
	defer notify.Stop()
	views := time.NewTicker(200 * time.Millisecond)
	defer views.Stop()
	viewPending := false
	defer flush()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-s.viewChanged:
			viewPending = true
		case <-views.C:
			if viewPending {
				viewPending = false
				if err := interest(); err != nil {
					return err
				}
			}
		case <-notify.C:
			flush()
		case item := <-messages:
			if item.err != nil {
				return item.err
			}
			message := item.message
			switch message.Type {
			case "chunk":
				if welcome.Delivery == "interest" && !r2link.Contains(ranges, message.X, message.Z) {
					continue
				}
				s.mu.Lock()
				keys, err := world.Apply(message)
				if err == nil {
					s.invalidateLocked(keys)
					for _, key := range keys {
						dirty[key] = true
					}
				}
				s.mu.Unlock()
				if err != nil {
					conn.Write(ctx, r2link.Message{Type: "error", Message: err.Error()})
					return err
				}
			case "camera.reset":
				if message.Position == nil {
					return fmt.Errorf("camera.reset 需要 position")
				}
				flush()
				s.mu.Lock()
				s.command++
				s.camera = &r2link.Camera{Position: *message.Position, Yaw: message.Yaw, Pitch: message.Pitch}
				s.current.info.Position = *message.Position
				s.broadcastLocked(event{
					Type: "camera", WorldID: s.current.info.ID,
					Camera: s.camera, Command: s.command,
				})
				s.mu.Unlock()
			case "scene.reset":
				clear(dirty)
				if message.Scene != nil {
					scene := *welcome.Scene
					if message.Scene.Name != "" {
						scene.Name = message.Scene.Name
					}
					if message.Scene.Dimension != "" {
						scene.Dimension = message.Scene.Dimension
					}
					if message.Scene.Height != 0 {
						scene.MinY, scene.Height = message.Scene.MinY, message.Scene.Height
					}
					welcome.Scene = &scene
				}
				welcome.Camera = message.Camera
				world, err = s.linkWorld(
					ctx,
					welcome,
					options,
					fix,
					message.Camera == nil,
				)
				if err != nil {
					return err
				}
				if err := ready(); err != nil {
					return err
				}
			case "error":
				return fmt.Errorf("数据源：%s", message.Message)
			default:
				return fmt.Errorf("不支持的来源消息 %q", message.Type)
			}
		}
	}
}

// linkWorld 在来源准备完成后交换会话，场景重置只换内存世界而保留连接。
func (s *Server) linkWorld(
	ctx context.Context,
	welcome r2link.Message,
	options ConnectOptions,
	fix, preserve bool,
) (*linksource.World, error) {
	world, err := linksource.New(
		welcome,
		s.mapping,
		fix,
		options.MemoryMiB<<20,
	)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		world.Close()
		return nil, err
	}
	s.revision++
	position := [3]float64{0, float64(welcome.Scene.MinY + welcome.Scene.Height/2), 0}
	if preserve {
		position = s.current.info.Position
	}
	if welcome.Camera != nil {
		position = welcome.Camera.Position
	}
	old := s.current
	info := Info{
		Info: worldinput.Info{
			Name: welcome.Scene.Name, Kind: "connection", Edition: welcome.Blocks,
			Dimension: welcome.Scene.Dimension, Position: position,
		},
		ID: s.revision, Loaded: true, MinY: welcome.Scene.MinY, Height: welcome.Scene.Height, BedrockFix: fix,
	}
	s.current = &session{info: info, source: world, cache: map[string]*list.Element{}, order: list.New()}
	s.connection.State = "connected"
	s.connection.Message = ""
	s.connection.Delivery = welcome.Delivery
	s.camera = welcome.Camera
	s.command++
	current := s.infoLocked()
	s.broadcastLocked(event{
		Type: "world", World: &current, Camera: s.camera,
		Command: s.command, PreserveCamera: preserve,
	})
	old.source.Close()
	return world, nil
}

// invalidateLocked 与来源提交共用总锁，旧构建不能在失效后写回缓存。
func (s *Server) invalidateLocked(keys [][2]int) {
	if len(keys) == 0 {
		return
	}
	s.current.version++
	s.current.chunks = nil
	for _, key := range keys {
		name := fmt.Sprintf("%d/%d", key[0], key[1])
		if element := s.current.cache[name]; element != nil {
			s.current.bytes -= len(element.Value.(cacheEntry).data)
			s.current.order.Remove(element)
			delete(s.current.cache, name)
		}
	}
}

// publishConnectionLocked 只改变 HUD 状态，断线不会重新创建浏览器流。
func (s *Server) publishConnectionLocked() {
	info := s.infoLocked()
	s.broadcastLocked(event{Type: "connection", World: &info})
}

// connect 接收网页选项，连接状态和握手结果经 SSE 返回所有页面。
func (s *Server) connect(w http.ResponseWriter, r *http.Request) {
	var options ConnectOptions
	if err := json.NewDecoder(r.Body).Decode(&options); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	info, err := s.Connect(options)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	writeJSON(w, info)
}
