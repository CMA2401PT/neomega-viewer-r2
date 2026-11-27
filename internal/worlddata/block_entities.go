package worlddata

import (
	"context"

	"celler"
	nbt "neomega-nbt/presets/v1"
	"neomega-r2link"
)

// BlockEntities 只遍历近景区块已有的方块实体；方块状态已在区域调色板中发送。
func (w *World) BlockEntities(ctx context.Context, rx, rz int, center [3]int, radius int) ([]r2link.BlockEntity, error) {
	output := []r2link.BlockEntity{}
	vertical := min(radius+1, 12)
	for x := rx * 8; x < (rx+1)*8; x++ {
		for z := rz * 8; z < (rz+1)*8; z++ {
			if (x-center[0])*(x-center[0])+(z-center[2])*(z-center[2]) > radius*radius {
				continue
			}
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			entities, err := w.ReadBlockEntities(ctx, celler.ChunkPos{X: int32(x), Z: int32(z)})
			if err != nil {
				return nil, err
			}
			for _, entity := range entities {
				sy := entity.Pos.Y >> 4
				if !visible(sy) || abs(int(sy)-center[1]) > vertical {
					continue
				}
				snbt, err := nbt.EncodeToSNBT(entity.NBT, nbt.OptEncodeToSNBT{SortMapKey: true})
				if err != nil {
					return nil, err
				}
				output = append(output, r2link.BlockEntity{
					Position: [3]int{int(entity.Pos.X), int(entity.Pos.Y), int(entity.Pos.Z)}, SNBT: snbt,
				})
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
