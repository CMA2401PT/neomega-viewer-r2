package worlddata

import (
	"celler"
	"celler/java"
	"celler/layer_storage"
	"context"
	"neomega-blocks/ir"
	converter "neomega-converter-core"
	nbt "neomega-nbt/presets/v1"

	"testing"
)

// 固定窗口的两端必须保留，越界 section 按区域计数，对象读取不受影响。
func TestHeightWindowAndDiagnostics(t *testing.T) {
	chunk := &converter.JavaChunk{Sections: map[int32]*converter.JavaSection{}}
	for _, y := range []int32{-5, -4, 27, 28} {
		chunk.Sections[y] = &converter.JavaSection{Blocks: layer_storage.MakePaletteStorage(ir.IRBlock("minecraft:stone []"))}
	}
	world := &World{
		ReadBlockEntities: func(ctx context.Context, pos celler.ChunkPos) ([]java.BlockEntity, error) {
			if pos != (celler.ChunkPos{}) {
				return nil, nil
			}
			return []java.BlockEntity{
				{Pos: celler.BlockPos{Y: 447}, NBT: nbt.Compound{"raw": int64(9007199254740993)}},
				{Pos: celler.BlockPos{Y: 448}, NBT: nbt.Compound{}},
			}, nil
		},
		ReadChunk: func(ctx context.Context, pos celler.ChunkPos) (*converter.JavaChunk, error) {
			if pos == (celler.ChunkPos{}) {
				return chunk, nil
			}
			return nil, nil
		},
	}
	for range 2 {
		region, err := world.Region(context.Background(), 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(region.Sections) != 2 || region.Clipped != 2 {
			t.Fatalf("窗口内 %d 个 section，越界 %d 个", len(region.Sections), region.Clipped)
		}
		objects, err := world.BlockEntities(context.Background(), 0, 0, [3]int{0, 27, 0}, 16)
		if err != nil || len(objects) != 1 || objects[0].SNBT != `{"raw": 9007199254740993l}` {
			t.Fatal(objects, err)
		}
	}
}
