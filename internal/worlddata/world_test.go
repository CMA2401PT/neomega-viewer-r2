package worlddata

import (
	"celler"
	"celler/layer_storage"
	"context"
	"neomega-blocks/ir"
	converter "neomega-converter-core"
	"neomega-r2link/blockactor"
	"neomega-r2link/effector"
	"testing"
)

// 固定窗口的两端必须保留，越界 section 按区域计数，对象读取不受影响。
func TestHeightWindowAndDiagnostics(t *testing.T) {
	chunk := &converter.JavaChunk{Sections: map[int32]*converter.JavaSection{}}
	for _, y := range []int32{-5, -4, 27, 28} {
		chunk.Sections[y] = &converter.JavaSection{Blocks: layer_storage.MakePaletteStorage(ir.IRBlock("minecraft:stone []"))}
	}
	world := &World{
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
		objects, err := world.Objects(context.Background(), 0, 0, [3]int{0, 27, 0}, 16, effector.Registry{}, blockactor.Registry{})
		if err != nil || len(objects) != 0 {
			t.Fatal(objects, err)
		}
	}
}
