package desktop

import (
	"fmt"
	"io"
	"strings"
)

// Choice 是一种浏览器与 ANGLE 图形后端的组合。不同电脑上哪种组合能用、哪种最快差别很大，
// 所以终端里把几种组合都列成按钮，打不开或很卡时由用户换一个。
type Choice struct {
	// Key 用于 -open 参数和浏览器配置目录名。
	Key     string
	Name    string
	Browser string // "edge" 或 "chrome"
	Angle   string
}

// Choices 的第一项是默认组合；找不到对应浏览器时按顺序尝试下一项。
var Choices = []Choice{
	{Key: "chrome", Name: "Chrome", Browser: "chrome"},
	{Key: "edge", Name: "Edge", Browser: "edge"},
	{Key: "chrome-gl", Name: "Chrome + OpenGL", Browser: "chrome", Angle: "gl"},
	{Key: "chrome-vulkan", Name: "Chrome + Vulkan", Browser: "chrome", Angle: "vulkan"},
	{Key: "edge-gl", Name: "Edge + OpenGL", Browser: "edge", Angle: "gl"},
	{Key: "edge-vulkan", Name: "Edge + Vulkan", Browser: "edge", Angle: "vulkan"},
}

// FindChoice 按 Key 查找组合。
func FindChoice(key string) (int, bool) {
	for i, choice := range Choices {
		if choice.Key == key {
			return i, true
		}
	}
	return 0, false
}

// buttonLabel 是按钮里的文字；点击判断靠在终端那一行里找到这段文字，所以只用 ASCII，一个字符占一格。
func buttonLabel(i int) string {
	return fmt.Sprintf("%d %s", i+1, Choices[i].Name)
}

// PrintChoices 每个组合占一行，推荐说明放在按钮后，保持按钮点击列与 ASCII 字符位置一致。
func PrintChoices(out io.Writer, colored, interactive bool) {
	if interactive {
		fmt.Fprintf(out, "  点击按钮或按数字键 1～%d 打开窗口：\n", len(Choices))
	} else {
		fmt.Fprintln(out, "  要在启动时打开窗口，可使用以下参数：")
	}
	for i, choice := range Choices {
		label := fmt.Sprintf("-browser -open %-14s %s", choice.Key, choice.Name)
		if interactive {
			label = "[" + buttonLabel(i) + "]"
			if colored {
				label = "\x1b[1;97;48;5;30m " + buttonLabel(i) + " \x1b[0m"
			}
		}
		if i == 0 {
			label += "（推荐）"
		}
		fmt.Fprintln(out, "    "+label)
	}
}

// hitButton 判断在终端某一行的第 column 格点击时命中了哪个按钮；row 是这一行的文字。
// 按钮两侧各多算一格，对应着色时的留白或方括号。
func hitButton(row string, column int) int {
	for i := range Choices {
		label := buttonLabel(i)
		start := strings.Index(row, label)
		if start >= 0 && column >= start-1 && column <= start+len(label) {
			return i
		}
	}
	return -1
}
