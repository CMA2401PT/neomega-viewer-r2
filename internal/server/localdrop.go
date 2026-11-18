package server

import (
	"neomega-viewer-r2/internal/filebrowse"
	"net"
	"net/http"
	"strings"
	"sync"
)

var localDropWindow sync.Mutex

// Require a loopback connection and Host before opening a native window.
func loopbackBrowser(r *http.Request) bool {
	remote, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil || !net.ParseIP(remote).IsLoopback() {
		return false
	}
	host, _, err := net.SplitHostPort(r.Host)
	if err != nil {
		return false
	}
	return strings.EqualFold(host, "localhost") || net.ParseIP(host).IsLoopback()
}

func localDrop(w http.ResponseWriter, r *http.Request) {
	if !loopbackBrowser(r) || r.Header.Get("Origin") != "http://"+r.Host {
		http.Error(w, "只能通过本机网页打开拖放窗口", http.StatusForbidden)
		return
	}
	if !filebrowse.NativeDropAvailable() {
		http.NotFound(w, r)
		return
	}
	if !localDropWindow.TryLock() {
		http.Error(w, "拖放窗口已打开", http.StatusConflict)
		return
	}
	defer localDropWindow.Unlock()
	path, err := filebrowse.NativeDrop(r.Context())
	if err != nil {
		if r.Context().Err() == nil {
			http.Error(w, "无法打开 Windows 拖放窗口，请直接输入路径", http.StatusInternalServerError)
		}
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, struct {
		Path string `json:"path"`
	}{Path: path})
}
