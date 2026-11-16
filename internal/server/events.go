package server

import (
	"encoding/json"
	"fmt"
	"net/http"

	"neomega-viewer-r2/r2link"
)

// event 用一条 SSE 顺序传递会话、区域及控制消息，区域内容仍走 HTTP。
type event struct {
	Type           string         `json:"type"`
	World          *Info          `json:"world,omitempty"`
	WorldID        uint64         `json:"worldId,omitempty"`
	Page           uint64         `json:"page,omitempty"`
	Version        uint64         `json:"version,omitempty"`
	Regions        [][2]int       `json:"regions,omitempty"`
	Camera         *r2link.Camera `json:"camera,omitempty"`
	Command        uint64         `json:"command,omitempty"`
	PreserveCamera bool           `json:"preserveCamera,omitempty"`
}

// pageView 的生命期就是 SSE 连接，断开后立刻从兴趣并集中移除。
type pageView struct {
	events chan event
	world  uint64
	view   r2link.View
	ranges []r2link.Range
}

// broadcastLocked 的慢页面重新同步当前快照，避免无限堆积更新。
func (s *Server) broadcastLocked(value event) {
	for id, page := range s.pages {
		select {
		case page.events <- value:
		default:
			close(page.events)
			delete(s.pages, id)
			s.interestChanged()
		}
	}
}

// interestChanged 合并页面的连续移动，发送循环读取最新完整范围。
func (s *Server) interestChanged() {
	select {
	case s.viewChanged <- struct{}{}:
	default:
	}
}

// events 在登记页面的同一个锁内取得首份快照，不会漏过中间提交。
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	s.mu.Lock()
	s.pageID++
	id := s.pageID
	page := &pageView{events: make(chan event, 64)}
	s.pages[id] = page
	info := s.infoLocked()
	initial := event{Type: "sync", Page: id, World: &info, Version: s.current.version, Camera: s.camera, Command: s.command}
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.pages, id); s.interestChanged(); s.mu.Unlock() }()
	send := func(value event) error {
		data, err := json.Marshal(value)
		if err != nil {
			return err
		}
		if _, err = fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
			return err
		}
		return http.NewResponseController(w).Flush()
	}
	if err := send(initial); err != nil {
		return
	}
	for {
		select {
		case <-r.Context().Done():
			return
		case value, ok := <-page.events:
			if !ok {
				return
			}
			if err := send(value); err != nil {
				return
			}
		}
	}
}

// view 接收页面实际需要的来源矩形，模型跨界和地图范围由网页统一计算。
func (s *Server) view(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Page   uint64         `json:"page"`
		World  uint64         `json:"world"`
		View   r2link.View    `json:"view"`
		Ranges []r2link.Range `json:"ranges"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	page := s.pages[request.Page]
	if page == nil || request.World != s.current.info.ID {
		http.Error(w, "页面或场景已经切换", 409)
		return
	}
	page.world, page.view, page.ranges = request.World, request.View, request.Ranges
	s.interestChanged()
	w.WriteHeader(http.StatusNoContent)
}
