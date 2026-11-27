package server

import (
	"io/fs"
	app "neomega-viewer-r2"
	"neomega-viewer-r2/internal/snapshot"
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

func testBrowser(t *testing.T) BrowserFiles {
	t.Helper()
	release, err := resources.Open()
	if err != nil {
		t.Fatal(err)
	}
	archive, recipe, err := release.Materials()
	if err != nil {
		t.Fatal(err)
	}
	player, err := snapshot.Player(archive)
	if err != nil {
		t.Fatal(err)
	}
	modules, err := fs.Sub(app.Web, "web/dist/r2")
	if err != nil {
		t.Fatal(err)
	}
	return BrowserFiles{Modules: modules, Materials: archive, Recipe: recipe, ExportTemplate: snapshot.Template(player)}
}
