// Package filebrowse 只解释本机路径和目录，不承担存档打开与渲染。
package filebrowse

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
)

type Entry struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	World bool   `json:"world"`
	Pack  bool   `json:"pack"`
}
type Listing struct {
	Path    string  `json:"path"`
	Parent  string  `json:"parent"`
	Roots   []Entry `json:"roots"`
	Entries []Entry `json:"entries"`
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
	entries, err := os.ReadDir(path)
	if err != nil {
		return Listing{}, err
	}
	out := Listing{Path: path, Parent: filepath.Dir(path), Entries: []Entry{}, Roots: []Entry{{Name: "主目录", Path: home}}}
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
		if !stat.IsDir() {
			if packs && strings.EqualFold(filepath.Ext(full), ".zip") {
				out.Entries = append(out.Entries, Entry{Name: entry.Name(), Path: full, Pack: true})
			}
			continue
		}
		_, packErr := os.Stat(filepath.Join(full, "assets"))
		_, levelErr := os.Stat(filepath.Join(full, "level.dat"))
		_, regionErr := os.Stat(filepath.Join(full, "region"))
		out.Entries = append(out.Entries, Entry{Name: entry.Name(), Path: full, World: levelErr == nil && regionErr == nil, Pack: packs && packErr == nil})
	}
	slices.SortFunc(out.Entries, func(a, b Entry) int { return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)) })
	return out, nil
}
