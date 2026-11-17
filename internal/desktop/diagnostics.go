package desktop

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Diagnostics 把启动环境、浏览器启动结果和网页上报的渲染事件写进一份文本日志。
// 每次启动重写，测试者遇到问题时把整份文件贴回即可。所有方法对 nil 安全。
type Diagnostics struct {
	mu    sync.Mutex
	file  *os.File
	start time.Time
	// Path 是日志的实际位置；找不到可写目录时为空。
	Path string
}

// OpenDiagnostics 依次尝试 exe 所在目录、用户缓存目录和临时目录，用第一个可写的。
func OpenDiagnostics(name string) *Diagnostics {
	var dirs []string
	if exe, err := os.Executable(); err == nil {
		dirs = append(dirs, filepath.Dir(exe))
	}
	if cache, err := os.UserCacheDir(); err == nil {
		dirs = append(dirs, filepath.Join(cache, "neomega-viewer-r2"))
	}
	dirs = append(dirs, filepath.Join(os.TempDir(), "neomega-viewer-r2"))
	for _, dir := range dirs {
		if os.MkdirAll(dir, 0o755) != nil {
			continue
		}
		path := filepath.Join(dir, name)
		file, err := os.Create(path)
		if err != nil {
			continue
		}
		return &Diagnostics{file: file, start: time.Now(), Path: path}
	}
	return &Diagnostics{start: time.Now()}
}

// Printf 写一行带启动后秒数的记录。
func (d *Diagnostics) Printf(format string, args ...any) {
	if d == nil || d.file == nil {
		return
	}
	line := fmt.Sprintf(format, args...)
	d.mu.Lock()
	defer d.mu.Unlock()
	fmt.Fprintf(d.file, "[%7.1fs] %s\n", time.Since(d.start).Seconds(), strings.TrimRight(line, "\n"))
}

// Write 让标准库 log 的输出也进入诊断日志。
func (d *Diagnostics) Write(p []byte) (int, error) {
	d.Printf("%s", p)
	return len(p), nil
}

// Environment 记录版本、系统、启动参数和找到的浏览器。
func (d *Diagnostics) Environment(version string) {
	d.Printf("neomega-viewer-r2 %s，%s/%s，%d 个逻辑处理器", version, runtime.GOOS, runtime.GOARCH, runtime.NumCPU())
	d.Printf("系统：%s", osVersion())
	d.Printf("启动时间：%s", d.start.Format(time.RFC3339))
	if exe, err := os.Executable(); err == nil {
		d.Printf("程序：%s", exe)
	}
	d.Printf("参数：%q", os.Args[1:])
	for _, line := range browserReport() {
		d.Printf("浏览器：%s", line)
	}
}

// Handler 接收网页上报的事件：{"event": 名称, ...其余字段}，原样写成一行 JSON。
func (d *Diagnostics) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var event struct {
			Event string `json:"event"`
		}
		if json.Unmarshal(body, &event) != nil {
			http.Error(w, "需要 JSON", http.StatusBadRequest)
			return
		}
		d.Printf("网页 %s %s", event.Event, body)
		w.WriteHeader(http.StatusNoContent)
	}
}

// Close 结束日志。
func (d *Diagnostics) Close() {
	if d == nil || d.file == nil {
		return
	}
	d.Printf("退出")
	d.mu.Lock()
	defer d.mu.Unlock()
	_ = d.file.Close()
	d.file = nil
}
