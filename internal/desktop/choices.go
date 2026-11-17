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
	{Key: "edge-vulkan", Name: "Edge + Vulkan", Browser: "edge", Angle: "vulkan"},
	{Key: "chrome-vulkan", Name: "Chrome + Vulkan", Browser: "chrome", Angle: "vulkan"},
	{Key: "chrome-gl", Name: "Chrome + OpenGL", Browser: "chrome", Angle: "gl"},
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

// PrintChoices 在终端里画出一排按钮。interactive 为假时（输入不是控制台）只列出组合和对应参数。
func PrintChoices(out io.Writer, colored, interactive bool) {
	if !interactive {
		fmt.Fprintln(out, "  打不开、黑屏、很卡或一直在编译时，可以换一种浏览器与图形后端，用 -open 参数指定：")
		for _, choice := range Choices {
			fmt.Fprintf(out, "    -open %-14s %s\n", choice.Key, choice.Name)
		}
		return
	}
	fmt.Fprintln(out, "  打不开、黑屏、很卡或一直在编译时，点下面的按钮换一种浏览器与图形后端（也可以按数字键 1～3）：")
	var row strings.Builder
	row.WriteString("  ")
	for i := range Choices {
		if colored {
			row.WriteString("\x1b[1;97;48;5;30m " + buttonLabel(i) + " \x1b[0m   ")
		} else {
			row.WriteString("[" + buttonLabel(i) + "]   ")
		}
	}
	fmt.Fprintln(out)
	fmt.Fprintln(out, strings.TrimRight(row.String(), " "))
	fmt.Fprintln(out)
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
