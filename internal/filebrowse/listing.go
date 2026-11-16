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
	Kind     string  `json:"kind"`
	Path     string  `json:"path"`
	Parent   string  `json:"parent"`
	Selected string  `json:"selected"`
	Roots    []Entry `json:"roots"`
	Entries  []Entry `json:"entries"`
}

// List 由 Go 处理盘符、父目录和符号链接，网页只使用返回的完整路径。
func List(path string, packs bool) (Listing, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Listing{}, err
	}
	if path == "" {
		path = home
	}
	path, err = filepath.Abs(strings.Trim(strings.TrimSpace(path), "\""))
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
	out := Listing{Selected: selected, Path: path, Parent: filepath.Dir(path), Entries: []Entry{}, Roots: []Entry{{Name: "主目录", Path: home}}}
	out.Kind = worldinput.DirectoryKind(path)
	if selected != "" && worldinput.FileKind(selected) != "level" {
		out.Kind = worldinput.FileKind(selected)
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
		if packs {
			if stat.IsDir() {
				_, err := os.Stat(filepath.Join(full, "assets"))
				value.Kind, value.Openable = "folder", err == nil
				if value.Openable {
					value.Kind = "texture"
				}
			} else if strings.EqualFold(filepath.Ext(full), ".zip") {
				value.Kind, value.Openable = "texture", true
			} else {
				continue
			}
		} else if stat.IsDir() {
			value.Kind = worldinput.DirectoryKind(full)
			value.Openable = value.Kind != "folder"
		} else {
			value.Kind = worldinput.FileKind(full)
			if value.Kind == "unknown" {
				continue
			}
			value.Openable = true
		}
		out.Entries = append(out.Entries, value)
	}
	slices.SortFunc(out.Entries, func(a, b Entry) int { return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)) })
	return out, nil
}
