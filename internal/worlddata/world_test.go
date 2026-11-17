package worlddata

import (
	"celler"
	"celler/layer_storage"
	"context"
	"encoding/binary"
	"neomega-blocks/ir"
	converter "neomega-converter-core"
	"neomega-r2/blockactor"
	"neomega-r2/effector"
	"testing"
)

// 固定窗口的两端必须保留，越界 section 在区域与对象读取之间只计数一次。
func TestHeightWindowAndDiagnostics(t *testing.T) {
	chunk := &converter.JavaChunk{Sections: map[int32]*converter.JavaSection{}}
	for _, y := range []int32{-5, -4, 27, 28} {
		chunk.Sections[y] = &converter.JavaSection{Blocks: layer_storage.MakePaletteStorage(ir.IRBlock("minecraft:stone []"))}
	}
	world := &World{
		HeightDiagnostics: true,
		ReadChunk: func(ctx context.Context, pos celler.ChunkPos) (*converter.JavaChunk, error) {
			if pos == (celler.ChunkPos{}) {
				return chunk, nil
			}
			return nil, nil
		},
	}
	for range 2 {
		data, err := world.Region(context.Background(), 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		if got := binary.LittleEndian.Uint32(data[4:]); got != 2 {
			t.Fatal("窗口内 section 数", got)
		}
		objects, err := world.Objects(context.Background(), 0, 0, [3]int{0, 27, 0}, 16, effector.Registry{}, blockactor.Registry{})
		if err != nil || len(objects) != 0 {
			t.Fatal(objects, err)
		}
		if got := world.ClippedSections(); got != 2 {
			t.Fatal("越界计数", got)
		}
	}
}
