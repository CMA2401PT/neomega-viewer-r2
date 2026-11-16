// Package server 管理存档会话与 HTTP；核心资源、来源和特殊对象处理器由应用组装。
package server

import (
	"container/list"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	r2 "neomega-r2"
	"neomega-r2/blockactor"
	"neomega-r2/effector"
	"neomega-r2/object"
	"neomega-viewer-r2/internal/filebrowse"
	"neomega-viewer-r2/internal/javaworld"
	"neomega-viewer-r2/internal/mobius"
	"net/http"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

// Info 描述当前可加载的来源；Loaded 同时适用于存档与内置场景。
type Info struct {
	Scene    *mobius.Scene `json:"scene,omitempty"`
	ID       uint64        `json:"id"`
	Loaded   bool          `json:"loaded"`
	Name     string        `json:"name"`
	Path     string        `json:"path"`
	Position [3]float64    `json:"position"`
}

// worldSource 只要求会话真正读取的内容，使内置场景与存档共享缓存和 HTTP 路径。
type worldSource interface {
	Region(context.Context, int, int) ([]byte, error)
	Chunks(context.Context) ([]byte, error)
	Objects(context.Context, int, int, [3]int, int, effector.Registry, blockactor.Registry) ([]object.Descriptor, error)
	Close() error
}

// cacheEntry 将区域字节与 LRU 位置关联，限制每个会话的缓存容量。
type cacheEntry struct {
	key  string
	data []byte
}

// session 隔离来源与缓存，切换后旧请求不能读取新场景。
type session struct {
	info   Info
	source worldSource
	mu     sync.Mutex
	cache  map[string]*list.Element
	order  *list.List
	bytes  int
	chunks []byte
}

// Server 串行切换来源，并限制区域读取的并发量。
type Server struct {
	mu       sync.RWMutex
	openMu   sync.Mutex
	current  *session
	builtin  *mobius.World
	revision uint64
	mapping  map[string]uint32
	workers  chan struct{}
	Effects  effector.Registry
	Actors   blockactor.Registry
}

// New 接收启动时准备好的状态映射，使准备进度与 HTTP 会话管理各自独立。
func New(mapping map[string]uint32, effects effector.Registry, actors blockactor.Registry) *Server {
	s := &Server{
		mapping: mapping,
		builtin: mobius.New(mapping),
		workers: make(chan struct{}, 4),
		Effects: effects,
		Actors:  actors,
	}
	s.Close()
	return s
}

// Open 先准备新来源，成功后等待旧读取完成再交换，打开失败时保留当前世界。
func (s *Server) Open(path string) (Info, error) {
	s.openMu.Lock()
	defer s.openMu.Unlock()
	path, err := filepath.Abs(path)
	if err != nil {
		return Info{}, err
	}
	if filepath.Base(path) == "level.dat" {
		path = filepath.Dir(path)
	}
	source, err := javaworld.OpenWorld(path, s.mapping)
	if err != nil {
		return Info{}, err
	}
	position, err := source.Position()
	if err != nil {
		source.Close()
		return Info{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.revision++
	info := Info{ID: s.revision, Loaded: true, Name: filepath.Base(path), Path: path, Position: position}
	old := s.current
	s.current = &session{info: info, source: source, cache: map[string]*list.Element{}, order: list.New()}
	if old != nil {
		old.source.Close()
	}
	return info, nil
}

// Info 让浏览器依据同一会话描述建立流式来源和开场取景。
func (s *Server) Info() Info {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.current.info
}

// Close 关闭当前存档并建立新的 Möbius 会话，旧会话请求随即失效。
func (s *Server) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current != nil {
		s.current.source.Close()
	}
	s.revision++
	scene := mobius.Opening()
	s.current = &session{
		info: Info{
			ID:       s.revision,
			Loaded:   true,
			Name:     "Möbius",
			Position: scene.Camera.Position,
			Scene:    scene,
		},
		source: s.builtin,
		cache:  map[string]*list.Element{},
		order:  list.New(),
	}
}
func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(value)
}
func queryInt(r *http.Request, name string) (int, error) {
	return strconv.Atoi(r.URL.Query().Get(name))
}

// Handler 让内置场景与 Java 存档使用相同接口，并提供公共渲染资源。
func (s *Server) Handler(web fs.FS) http.Handler {
	mux := http.NewServeMux()
	assets, _ := fs.Sub(r2.Files, "assets")
	core, _ := fs.Sub(r2.Files, "web")
	mux.Handle("/r2/assets/", http.StripPrefix("/r2/assets/", http.FileServerFS(assets)))
	mux.Handle("/r2/", http.StripPrefix("/r2/", http.FileServerFS(core)))
	mux.HandleFunc("GET /api/world", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, s.Info()) })
	mux.HandleFunc("POST /api/world/open", func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Path string `json:"path"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		info, err := s.Open(request.Path)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		writeJSON(w, info)
	})
	mux.HandleFunc("POST /api/world/close", func(w http.ResponseWriter, r *http.Request) { s.Close(); writeJSON(w, s.Info()) })
	mux.HandleFunc("GET /api/files", func(w http.ResponseWriter, r *http.Request) {
		listing, err := filebrowse.List(r.URL.Query().Get("path"), r.URL.Query().Get("kind") == "pack")
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		writeJSON(w, listing)
	})
	mux.HandleFunc("POST /api/textures", func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Path    string `json:"path"`
			MaxSize int    `json:"maxSize"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		textures, err := r2.BuildTextures(request.Path, request.MaxSize)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		textures.WriteTo(w)
	})
	mux.HandleFunc("GET /api/world/region", s.region)
	mux.HandleFunc("GET /api/world/chunks", s.chunks)
	mux.HandleFunc("GET /api/world/objects", s.objects)
	mux.Handle("/", http.FileServerFS(web))
	return mux
}

// read 保证一次构建只接触一个世界；等待中的旧代请求在取得工作名额后返回 409。
func (s *Server) read(w http.ResponseWriter, r *http.Request, build func(*session) (any, error)) {
	id, err := strconv.ParseUint(r.URL.Query().Get("world"), 10, 64)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	select {
	case s.workers <- struct{}{}:
	case <-r.Context().Done():
		return
	}
	defer func() { <-s.workers }()
	start := time.Now()
	s.mu.RLock()
	if s.current == nil || s.current.info.ID != id {
		s.mu.RUnlock()
		http.Error(w, "存档已经切换", 409)
		return
	}
	value, err := build(s.current)
	s.mu.RUnlock()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Server-Timing", fmt.Sprintf("read;dur=%.3f", float64(time.Since(start).Microseconds())/1000))
	if data, ok := value.([]byte); ok {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write(data)
	} else {
		writeJSON(w, value)
	}
}

// chunks 的会话缓存独立于区域 LRU，缩略地图在同一会话内只需扫描一次占用表。
func (s *Server) chunks(w http.ResponseWriter, r *http.Request) {
	s.read(w, r, func(current *session) (any, error) {
		current.mu.Lock()
		defer current.mu.Unlock()
		if current.chunks == nil {
			data, err := current.source.Chunks(r.Context())
			if err != nil {
				return nil, err
			}
			current.chunks = data
		}
		return current.chunks, nil
	})
}

func (s *Server) region(w http.ResponseWriter, r *http.Request) {
	x, err := queryInt(r, "x")
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	z, err := queryInt(r, "z")
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	key := fmt.Sprintf("%d/%d", x, z)
	s.read(w, r, func(current *session) (any, error) {
		current.mu.Lock()
		element := current.cache[key]
		if element != nil {
			current.order.MoveToFront(element)
			data := element.Value.(cacheEntry).data
			current.mu.Unlock()
			return data, nil
		}
		current.mu.Unlock()
		data, err := current.source.Region(r.Context(), x, z)
		if err != nil {
			return nil, err
		}
		current.mu.Lock()
		defer current.mu.Unlock()
		if _, exists := current.cache[key]; !exists {
			current.cache[key] = current.order.PushFront(cacheEntry{key: key, data: data})
			current.bytes += len(data)
		}
		for current.bytes > 64*1024*1024 && current.order.Len() > 1 {
			tail := current.order.Back()
			entry := tail.Value.(cacheEntry)
			delete(current.cache, entry.key)
			current.order.Remove(tail)
			current.bytes -= len(entry.data)
		}
		return data, nil
	})
}
func (s *Server) objects(w http.ResponseWriter, r *http.Request) {
	values := map[string]int{}
	for _, name := range []string{"x", "z", "cx", "cy", "cz", "radius"} {
		value, err := queryInt(r, name)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		values[name] = value
	}
	if values["radius"] < 1 || values["radius"] > 16 {
		http.Error(w, "近景视距应为 1～16 区块", 400)
		return
	}
	s.read(w, r, func(current *session) (any, error) {
		return current.source.Objects(
			r.Context(),
			values["x"],
			values["z"],
			[3]int{values["cx"], values["cy"], values["cz"]},
			values["radius"],
			s.Effects,
			s.Actors,
		)
	})
}
