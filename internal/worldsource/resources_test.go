package worldsource

import (
	"neomega-viewer-r2/resources"
	"testing"
)

// 测试从应用入口使用的发布包取表，覆盖真实的资源装配路径。
func testWorlds(t *testing.T) Opener {
	t.Helper()
	release, err := resources.Open()
	if err != nil {
		t.Fatal(err)
	}
	return Opener{Tables: release.Tables, Fixer: release.Fixer}
}
