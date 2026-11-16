package desktop

import (
	"fmt"
	"io"
)

// Progress 将启动阶段显示在同一行；重定向输出时每个阶段独占一行。
type Progress struct {
	Out     io.Writer
	Colored bool
}

// Step 在耗时操作开始前显示它的名字，让等待中的用户知道正在准备什么。
func (p Progress) Step(name string) {
	if p.Colored {
		fmt.Fprintf(p.Out, "\r\x1b[2K  \x1b[38;5;43m%s\x1b[0m", name)
	} else {
		fmt.Fprintln(p.Out, "  "+name)
	}
}

// Done 结束进度行，为下一行的就绪地址留出位置。
func (p Progress) Done() {
	p.Step("完成！")
	if p.Colored {
		fmt.Fprintln(p.Out)
	}
}
