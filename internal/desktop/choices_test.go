package desktop

import (
	"strings"
	"testing"
)

// 按钮行按读回的文字判断点击位置；按钮文字两侧各多算一格。
func TestHitButton(t *testing.T) {
	var output strings.Builder
	PrintChoices(&output, false, true)
	rows := strings.Split(strings.TrimSpace(output.String()), "\n")
	for i := range Choices {
		row := rows[i+1]
		start := strings.Index(row, buttonLabel(i))
		for _, column := range []int{start - 1, start, start + len(buttonLabel(i)) - 1, start + len(buttonLabel(i))} {
			if got := hitButton(row, column); got != i {
				t.Fatalf("第 %d 列应命中按钮 %d，得到 %d（行 %q）", column, i, got, row)
			}
		}
	}
	if got := hitButton(rows[1], 0); got != -1 {
		t.Fatalf("行首空白不应命中按钮，得到 %d", got)
	}
	if got := hitButton("  打不开时点下面的按钮", 4); got != -1 {
		t.Fatalf("提示行不应命中按钮，得到 %d", got)
	}
}
