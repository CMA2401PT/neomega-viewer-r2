package snapshot

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"celler"
	"celler/bedrock"
	"celler/layer_storage"
	nbt "neomega-nbt/presets/v1"
	"neomega-r2link"
)

// 用真实结构文件走命令入口。两个方块相隔五个 r2region，不能只导出初始视野。
func TestStructureWithCustomTemplate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wide.nbt")
	data, err := nbt.EncodeToNBTBytes(map[string]any{
		"size":    []int32{641, 1, 1},
		"palette": []nbt.Compound{{"Name": "minecraft:stone"}},
		"blocks": []nbt.Compound{
			{"pos": []int32{0, 0, 0}, "state": int32(0)},
			{"pos": []int32{640, 0, 0}, "state": int32(0)},
		},
	}, nbt.OptEncodeToNBT{Encoding: nbt.EncodingBigEndian})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	template := filepath.Join(dir, "template.html")
	if err := os.WriteFile(template, []byte("<main>"+worldMarker+"</main>"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Run(context.Background(), []string{"-template", template, path}); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "wide.html")
	html, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(html, []byte("<main>")) || !bytes.HasSuffix(html, []byte("</main>")) {
		t.Fatal("模板内容未保留")
	}
	regions := regexp.MustCompile(`data-r2-region="([^"]+)">([^<]+)</script>`).FindAllSubmatch(html, -1)
	if len(regions) != 2 {
		t.Fatalf("期望两个非空区域，实际 %d", len(regions))
	}
	for i, match := range regions {
		if string(match[1]) != []string{"0/0", "5/0"}[i] {
			t.Fatalf("区域位置错误：%s", match[1])
		}
		packed, err := base64.StdEncoding.DecodeString(string(match[2]))
		if err != nil {
			t.Fatal(err)
		}
		z, err := gzip.NewReader(bytes.NewReader(packed))
		if err != nil {
			t.Fatal(err)
		}
		buffer, err := io.ReadAll(z)
		if err != nil {
			t.Fatal(err)
		}
		z.Close()
		if binary.LittleEndian.Uint32(buffer) != r2link.RegionMagic || binary.LittleEndian.Uint32(buffer[4:]) != 1 {
			t.Fatal("区域未按远景子区块格式写出")
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "wide")); !os.IsNotExist(err) {
		t.Fatal("输入旁留下了转换存档")
	}

	// 转换失败时已有输出应保持完整。
	if err := os.WriteFile(path, []byte("invalid nbt"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Run(context.Background(), []string{"-o", out, path}); err == nil {
		t.Fatal("损坏输入应报告错误")
	}
	after, err := os.ReadFile(out)
	if err != nil || !bytes.Equal(html, after) {
		t.Fatal("失败时改写了已有输出")
	}
}

func TestDefaultTemplateIncludesOfflinePlayer(t *testing.T) {
	if strings.Count(defaultTemplate, worldMarker) != 1 || strings.Count(defaultTemplate, viewerMarker) != 1 {
		t.Fatal("内置模板的注入标记不完整")
	}
	for _, id := range []string{"r2-worker", "r2-resources", "view", "teleport"} {
		if !strings.Contains(player, `id="`+id+`"`) {
			t.Fatalf("内嵌播放器缺少 %s", id)
		}
	}
}

func TestBedrockDimensionsAndNegativeChunks(t *testing.T) {
	dir := t.TempDir()
	source, err := bedrock.Open(dir, bedrock.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, dim := range []int32{0, 2} {
		chunk := bedrock.NewChunk(celler.ChunkPos{X: -9, Z: -1}, dim)
		layer := bedrock.NewLayer(bedrock.Block{State: "minecraft:air []", Version: 18168865})
		layer.Blocks.Set(layer_storage.At(0, 0, 0), bedrock.Block{State: "minecraft:stone []", Version: 18168865})
		if err := chunk.SetSection(&bedrock.Section{Y: 0, Version: 9, Layers: []*bedrock.Layer{layer}}); err != nil {
			t.Fatal(err)
		}
		if err := source.WriteChunk(chunk, nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := source.Close(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		dimension string
		regions   int
	}{{"", 2}, {"2", 1}} {
		var html bytes.Buffer
		if err := writeWorld(context.Background(), &html, dir, "Bedrock", tc.dimension); err != nil {
			t.Fatal(err)
		}
		if got := strings.Count(html.String(), `data-r2-region="-2/-1"`); got != tc.regions {
			t.Fatalf("维度 %q 应有 %d 个区域，得到 %d", tc.dimension, tc.regions, got)
		}
		if strings.Contains(html.String(), `"id":"1"`) {
			t.Fatal("空维度不应出现在清单中")
		}
	}
}
