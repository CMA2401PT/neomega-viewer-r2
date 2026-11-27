// Package resources 打开本应用发布的 Minecraft 资源。程序入口按用途取表，再把它们交给业务代码。
package resources

import (
	"archive/zip"
	"bytes"
	_ "embed"
	"io/fs"
	"neomega-converter-core/conversiondata"
	fixer "neomega-view-utils/fixer/java-postprocess"
	"neomega-view-utils/fixer/solidtable"
	"neomega-view-utils/r2assets"
)

//go:generate python3 generate.py

//go:embed minecraft.zip
var minecraft []byte

// Release 中的转换表按需解码，同一次运行中的多个任务共用已加载的表。
type Release struct {
	Tables *conversiondata.Loader
	files  fs.FS
}

// Open 读取随程序发布的资源目录，转换表只在相应输入格式首次用到时加载。
func Open() (*Release, error) {
	archive, err := zip.NewReader(bytes.NewReader(minecraft), int64(len(minecraft)))
	if err != nil {
		return nil, err
	}
	tables, err := fs.Sub(archive, "tables")
	if err != nil {
		return nil, err
	}
	return &Release{Tables: conversiondata.NewLoader(tables), files: archive}, nil
}

// Fixer 使用同一发布包中的实心面、状态、属性域和默认属性构造邻接修复器。
func (r *Release) Fixer() (*fixer.Fixer, error) {
	f, err := r.files.Open("solid-faces/solid-faces.nvsolid")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	solid, err := solidtable.Decode(f)
	if err != nil {
		return nil, err
	}
	schema, err := r.Tables.Schema()
	if err != nil {
		return nil, err
	}
	states, err := r.Tables.Waterlogged()
	if err != nil {
		return nil, err
	}
	defaults, err := r.Tables.JavaDefaults()
	if err != nil {
		return nil, err
	}
	return fixer.New(solid, schema, states, defaults), nil
}

// Materials 打开默认材质，并返回同版本的材质包配方供 HTTP 服务使用。
func (r *Release) Materials() (*r2assets.Archive, []byte, error) {
	materials, err := fs.Sub(r.files, "browser-materials")
	if err != nil {
		return nil, nil, err
	}
	data, err := fs.ReadFile(materials, "default-materials.zip")
	if err != nil {
		return nil, nil, err
	}
	archive, err := r2assets.OpenArchive(data)
	if err != nil {
		return nil, nil, err
	}
	recipe, err := fs.ReadFile(materials, "material-info.zip")
	return archive, recipe, err
}
