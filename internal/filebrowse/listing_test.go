package filebrowse

import (
	"os"
	"path/filepath"
	"testing"
)

// 粘贴文件路径后仍应保留选中项，材质包与世界来源在各自面板中识别。
func TestSourceKindsAndSelectedFile(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"world.pack", "a.schem", "b.schematic", "c.litematic", "d.nbt", "e.mcstructure", "f.mcworld", "texture.zip", "ignore.txt"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte{1}, 0644); err != nil {
			t.Fatal(err)
		}
	}
	for _, dir := range []string{"java/region", "bedrock/db", "texture/assets"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0755); err != nil {
			t.Fatal(err)
		}
	}
	selected := filepath.Join(root, "world.pack")
	listing, err := List(selected, false)
	if err != nil {
		t.Fatal(err)
	}
	if listing.Selected != selected || listing.Path != root || listing.Kind != "pack" {
		t.Fatal(listing)
	}
	expected := map[string]string{"world.pack": "pack", "a.schem": "schem", "b.schematic": "schematic", "c.litematic": "litematic", "d.nbt": "nbt", "e.mcstructure": "mcstructure", "f.mcworld": "mcworld", "java": "directory", "bedrock": "bedrock", "texture": "folder"}
	if len(listing.Entries) != len(expected) {
		t.Fatal(listing.Entries)
	}
	for _, entry := range listing.Entries {
		if expected[entry.Name] != entry.Kind || entry.Openable != (entry.Kind != "folder") {
			t.Fatal(entry)
		}
	}
	listing, err = List(root, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range listing.Entries {
		if entry.Openable && entry.Kind != "texture" {
			t.Fatal(entry)
		}
	}
}
