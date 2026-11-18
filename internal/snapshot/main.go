// Package snapshot 将存档中的静态方块写入可直接离线打开的 HTML。
package snapshot

import (
	"bufio"
	"context"
	_ "embed"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"neomega-converter-core/structure"
	"neomega-converter-core/worldinput"
)

//go:embed template.html
var defaultTemplate string

//go:embed web/dist/player.html
var player string

const worldMarker = "<!-- R2_WORLD -->"
const viewerMarker = "<!-- R2_VIEWER -->"

// Template 为 HUD 下载提供与命令行相同的离线播放器。
func Template() string { return strings.Replace(defaultTemplate, viewerMarker, player, 1) }

// Run 执行 viewer export 子命令。
func Run(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("viewer export", flag.ContinueOnError)
	output := flags.String("o", "", "输出 HTML 路径，默认为输入旁的同名 .html")
	custom := flags.String("template", "", "模板 HTML 路径，默认使用内嵌模板")
	dimension := flags.String("dimension", "", "仅导出这个维度，默认导出全部非空维度")
	flags.Usage = func() {
		fmt.Fprintln(flags.Output(), "用法：viewer export [选项] <存档目录、PackFS 或结构文件>")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		flags.Usage()
		return fmt.Errorf("需要一个输入路径")
	}
	input, err := worldinput.Resolve(flags.Arg(0))
	if err != nil {
		return err
	}
	name := filepath.Base(input.Path)
	isDirectory := input.Kind == "directory" || input.Kind == "bedrock"
	if !isDirectory {
		name = strings.TrimSuffix(name, filepath.Ext(name))
	}
	if *output == "" {
		*output = filepath.Join(filepath.Dir(input.Path), name+".html")
	}

	page := defaultTemplate
	if *custom != "" {
		data, err := os.ReadFile(*custom)
		if err != nil {
			return err
		}
		page = string(data)
	}
	before, after, found := strings.Cut(page, worldMarker)
	if !found || strings.Contains(after, worldMarker) || strings.Count(page, viewerMarker) > 1 {
		return fmt.Errorf("模板需要恰好一个 %s，且最多一个 %s", worldMarker, viewerMarker)
	}
	// 即便输出路径经过符号链接，也不能覆盖输入或模板。
	if existing, err := os.Stat(*output); err == nil {
		for _, path := range []string{flags.Arg(0), input.Path, *custom} {
			if info, err := os.Stat(path); err == nil && os.SameFile(info, existing) {
				return fmt.Errorf("输出路径与输入或模板相同：%s", *output)
			}
		}
	}

	source := input.Path
	if !isDirectory && structure.FileKind(source) != "" {
		temp, err := os.MkdirTemp(filepath.Dir(*output), ".r2-structure-*")
		if err != nil {
			return err
		}
		defer os.RemoveAll(temp)
		fmt.Fprintln(os.Stderr, "正在转换结构…")
		result, err := structure.ConvertFile(ctx, source, filepath.Join(temp, "world"))
		if err != nil {
			return err
		}
		source = result.Directory
	}
	// 扫描或写盘失败时保留已有 HTML。临时文件与最终输出同卷，完成后才发布。
	file, err := os.CreateTemp(filepath.Dir(*output), ".r2-export-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	writer := bufio.NewWriter(file)
	if _, err := io.WriteString(writer, strings.Replace(before, viewerMarker, player, 1)); err != nil {
		return err
	}
	if err := writeWorld(ctx, writer, source, name, *dimension); err != nil {
		return err
	}
	if _, err := io.WriteString(writer, strings.Replace(after, viewerMarker, player, 1)); err != nil {
		return err
	}
	if err := writer.Flush(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Rename(file.Name(), *output); err != nil {
		return err
	}
	fmt.Println(*output)
	return nil
}
