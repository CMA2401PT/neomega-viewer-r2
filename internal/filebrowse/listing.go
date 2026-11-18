// Package filebrowse 只解释本机路径和目录，不承担存档打开与渲染。
package filebrowse

import (
	"neomega-converter-core/worldinput"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
)

// Entry 的可打开状态来自服务端，页面不按后缀猜测存档类型。
type Entry struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	Directory bool   `json:"directory"`
	Kind      string `json:"kind"`
	Openable  bool   `json:"openable"`
}

// Listing 同时返回选中的文件与所在目录，粘贴文件路径后仍可浏览相邻来源。
type Listing struct {
	Kind       string  `json:"kind"`
	Path       string  `json:"path"`
	Parent     string  `json:"parent"`
	Selected   string  `json:"selected"`
	Roots      []Entry `json:"roots"`
	Entries    []Entry `json:"entries"`
	NativeDrop bool    `json:"nativeDrop"`
}

// List 由 Go 处理盘符、父目录和符号链接，网页只使用返回的完整路径。
func List(path string, packs bool) (Listing, error) {
	home, _ := os.UserHomeDir()
	work, workErr := os.Getwd()
	path = strings.Trim(strings.TrimSpace(path), "\"")
	if path == "" {
		path = work
		if workErr != nil {
			if home == "" {
				return Listing{}, workErr
			}
			path = home
		}
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return Listing{}, err
	}
	selected := ""
	if stat, err := os.Stat(path); err == nil && !stat.IsDir() {
		selected, path = path, filepath.Dir(path)
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return Listing{}, err
	}
	out := Listing{Selected: selected, Path: path, Parent: filepath.Dir(path), Entries: []Entry{}, Roots: []Entry{}}
	if workErr == nil {
		out.Roots = append(out.Roots, Entry{Name: "工作目录", Path: work})
	}
	if home != "" && home != work {
		out.Roots = append(out.Roots, Entry{Name: "主目录", Path: home})
	}
	out.Kind = directoryKind(path, packs)
	if selected != "" {
		if kind := fileKind(selected, packs); kind != "level" {
			out.Kind = kind
		}
	}
	if runtime.GOOS == "windows" {
		for c := 'A'; c <= 'Z'; c++ {
			root := string(c) + ":\\"
			if stat, err := os.Stat(root); err == nil && stat.IsDir() {
				out.Roots = append(out.Roots, Entry{Name: root, Path: root})
			}
		}
	} else {
		out.Roots = append(out.Roots, Entry{Name: "/", Path: "/"})
	}
	for _, entry := range entries {
		full := filepath.Join(path, entry.Name())
		stat, err := os.Stat(full)
		if err != nil {
			continue
		}
		value := Entry{Name: entry.Name(), Path: full, Directory: stat.IsDir()}
		if stat.IsDir() {
			value.Kind = directoryKind(full, packs)
			value.Openable = value.Kind != "folder"
		} else {
			value.Kind = fileKind(full, packs)
			if value.Kind == "unknown" {
				continue
			}
			value.Openable = true
		}
		out.Entries = append(out.Entries, value)
	}
	slices.SortFunc(out.Entries, func(a, b Entry) int {
		if a.Openable != b.Openable {
			if a.Openable {
				return -1
			}
			return 1
		}
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	return out, nil
}

func directoryKind(path string, packs bool) string {
	if !packs {
		return worldinput.DirectoryKind(path)
	}
	if stat, err := os.Stat(filepath.Join(path, "assets")); err == nil && stat.IsDir() {
		return "texture"
	}
	return "folder"
}

func fileKind(path string, packs bool) string {
	if !packs {
		return worldinput.FileKind(path)
	}
	if strings.EqualFold(filepath.Ext(path), ".zip") {
		return "texture"
	}
	return "unknown"
}
