package snapshot

import (
	"neomega-viewer-r2/internal/worldsource"
	"neomega-viewer-r2/resources"
	"testing"
)

// 测试从应用入口使用的发布包取表，覆盖真实的资源装配路径。
func testWorlds(t *testing.T) worldsource.Opener {
	t.Helper()
	release, err := resources.Open()
	if err != nil {
		t.Fatal(err)
	}
	return worldsource.Opener{Tables: release.Tables, Fixer: release.Fixer}
}

func testPlayer(t *testing.T) string {
	t.Helper()
	release, err := resources.Open()
	if err != nil {
		t.Fatal(err)
	}
	archive, _, err := release.Materials()
	if err != nil {
		t.Fatal(err)
	}
	player, err := Player(archive)
	if err != nil {
		t.Fatal(err)
	}
	return player
}
