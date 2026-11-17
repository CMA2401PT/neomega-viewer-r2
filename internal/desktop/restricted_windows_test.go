//go:build windows

package desktop

import "testing"

// 开发沙箱在工作区根目录显式加了拒绝项和低完整性标签；子目录只继承（带 ID），普通目录只有继承项。
func TestExplicitRestriction(t *testing.T) {
	for _, c := range []struct {
		sddl string
		want bool
	}{
		{"D:PAI(D;CI;DC;;;WD)(A;OICI;0x1301bf;;;S-1-5-21-1-2-3-4)(A;ID;FA;;;BA)S:(ML;OICI;NW;;;LW)", true},
		{"D:AI(D;CIID;DC;;;WD)(A;OICIID;0x1301bf;;;S-1-5-21-1-2-3-4)(A;ID;FA;;;BA)S:AI(ML;OICIID;NW;;;LW)", false},
		{"D:AI(A;ID;FA;;;BA)(A;OICIIOID;GA;;;BA)(A;ID;FA;;;SY)(A;ID;0x1301bf;;;AU)", false},
		{"D:(A;OICI;FA;;;SY)S:(ML;OICI;NW;;;LW)", true},
	} {
		if got := explicitRestriction(c.sddl); got != c.want {
			t.Errorf("%s：期望 %v，得到 %v", c.sddl, c.want, got)
		}
	}
	if dir, sddl := RestrictedLocation(); dir != "" {
		t.Logf("当前测试程序位于带特殊权限的目录：%s %s", dir, sddl)
	}
}
