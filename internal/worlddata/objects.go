package worlddata

import (
	"celler"
	"celler/layer_storage"
	"context"
	"neomega-r2link/blockactor"
	"neomega-r2link/effector"
	"neomega-r2link/object"
)

// Objects 只在近景调用已注册处理器，让方块外观与动画描述独立于静态区域。
func (w *World) Objects(ctx context.Context, rx, rz int, center [3]int, radius int, effects effector.Registry, actors blockactor.Registry) ([]object.Descriptor, error) {
	output := []object.Descriptor{}
	vertical := min(radius+1, 12)
	for x := rx * 8; x < (rx+1)*8; x++ {
		for z := rz * 8; z < (rz+1)*8; z++ {
			if (x-center[0])*(x-center[0])+(z-center[2])*(z-center[2]) > radius*radius {
				continue
			}
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			chunk, err := w.ReadChunk(ctx, celler.ChunkPos{X: int32(x), Z: int32(z)})
			if err != nil {
				return nil, err
			}
			if chunk == nil {
				continue
			}
			entities := chunk.BlockEntities
			nbts := map[[3]int]blockactor.Input{}
			for _, entry := range entities {
				p := [3]int{int(entry.Pos.X), int(entry.Pos.Y), int(entry.Pos.Z)}
				nbts[p] = blockactor.Input{Position: p, NBT: entry.NBT}
			}
			for y, section := range chunk.Sections {
				if !visible(y) || abs(int(y)-center[1]) > vertical {
					continue
				}
				if section.Blocks == nil {
					continue
				}
				palette := section.Blocks.GetUsedPalette()
				claimed := make([]bool, len(palette))
				any := false
				for i, state := range palette {
					_, effect := effects[state.NameWithNameSpace()]
					_, actor := actors[state.NameWithNameSpace()]
					claimed[i] = effect || actor
					any = any || claimed[i]
				}
				if !any {
					continue
				}
				for bx := 0; bx < 16; bx++ {
					for bz := 0; bz < 16; bz++ {
						for by := 0; by < 16; by++ {
							index := section.Blocks.GetIndex(layer_storage.At(bx, by, bz))
							if !claimed[index] {
								continue
							}
							state := palette[index]
							position := [3]int{x*16 + bx, int(y)*16 + by, z*16 + bz}
							descriptor, err := effects.Build(position, state)
							if err != nil {
								return nil, err
							}
							if descriptor != nil {
								output = append(output, *descriptor)
							}
							input := nbts[position]
							input.Position = position
							input.State = state
							descriptors, err := actors.Build(input)
							if err != nil {
								return nil, err
							}
							output = append(output, descriptors...)
						}
					}
				}
			}
		}
	}
	return output, nil
}
func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
