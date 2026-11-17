package worldsource

import (
	"celler"
	"celler/bedrock"
	"celler/layer_storage"
	"context"
	"neomega-converter-core/worldinput"
	"strconv"
	"testing"
)

// 跨区块的栅栏连接同时检验维度隔离、Bedrock 转换和修复开关的组装。
func TestBedrockDimensionsAndFix(t *testing.T) {
	path := t.TempDir()
	source, err := bedrock.Open(path, bedrock.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for dim := int32(0); dim < 3; dim++ {
		for x := int32(0); x < 2; x++ {
			chunk := bedrock.NewChunk(celler.ChunkPos{X: x}, dim)
			layer := bedrock.NewLayer(bedrock.Block{State: "minecraft:air []", Version: 18168865})
			localX := 15
			if x == 1 {
				localX = 0
			}
			layer.Blocks.Set(layer_storage.At(localX, 4, 8), bedrock.Block{State: "minecraft:oak_fence []", Version: 18168865})
			if err := chunk.SetSection(&bedrock.Section{Y: dim, Version: 9, Layers: []*bedrock.Layer{layer}}); err != nil {
				t.Fatal(err)
			}
			if err := source.WriteChunk(chunk, nil); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := source.Close(); err != nil {
		t.Fatal(err)
	}
	for dim := int32(0); dim < 3; dim++ {
		for _, skip := range []bool{false, true} {
			world, err := Open(context.Background(), Options{
				Options:        worldinput.Options{Path: path, Dimension: strconv.Itoa(int(dim))},
				SkipBedrockFix: skip,
			})
			if err != nil {
				t.Fatal(err)
			}
			chunk, err := world.ReadChunk(context.Background(), celler.ChunkPos{})
			if err != nil {
				t.Fatal(err)
			}
			if len(chunk.Sections) != 1 || chunk.Sections[dim] == nil {
				t.Fatal(chunk.Sections)
			}
			state := chunk.Sections[dim].Blocks.Get(layer_storage.At(15, 4, 8))
			east, _ := state.States().Get("east")
			if east != strconv.FormatBool(!skip) {
				t.Fatalf("dimension=%d skip=%v: %s", dim, skip, state)
			}
			if err := world.Close(); err != nil {
				t.Fatal(err)
			}
		}
	}
}
