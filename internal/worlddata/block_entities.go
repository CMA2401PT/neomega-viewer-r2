package worlddata

import (
	"context"
	"fmt"
	"iter"
	"math"

	"celler"
	nbt "neomega-nbt/presets/v1"
	"neomega-r2link"
)

// BlockEntities 只遍历近景区块已有的方块实体；方块状态已在区域调色板中发送。
func (w *World) BlockEntities(ctx context.Context, rx, rz int, center [3]int, radius int) ([]r2link.BlockEntity, error) {
	output := []r2link.BlockEntity{}
	vertical := min(radius+1, 12)
	for pos := range nearChunks(rx, rz, center, radius) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		entities, err := w.ReadBlockEntities(ctx, pos)
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
	return output, nil
}

// Entities 按 Java Pos 限制订阅高度，并把原文交给前端按实体类型解释。
func (w *World) Entities(ctx context.Context, rx, rz int, center [3]int, radius int) ([]string, error) {
	var output []string
	for pos := range nearChunks(rx, rz, center, radius) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		entities, err := w.ReadEntities(ctx, pos)
		if err != nil {
			return nil, err
		}
		for _, entity := range entities {
			position, ok := entity["Pos"].([]float64)
			if !ok || len(position) != 3 {
				return nil, fmt.Errorf("区块 %v 的 Java 实体缺少 Pos 坐标", pos)
			}
			sy := int(math.Floor(position[1] / 16))
			if !visible(int32(sy)) || abs(sy-center[1]) > min(radius+1, 12) {
				continue
			}
			text, err := nbt.EncodeToSNBT(entity, nbt.OptEncodeToSNBT{SortMapKey: true})
			if err != nil {
				return nil, err
			}
			output = append(output, text)
		}
	}
	return output, nil
}

// 两类近景数据共用圆形水平范围，圆心与半径均以区块为单位。
func nearChunks(rx, rz int, center [3]int, radius int) iter.Seq[celler.ChunkPos] {
	return func(yield func(celler.ChunkPos) bool) {
		for x := rx * 8; x < (rx+1)*8; x++ {
			for z := rz * 8; z < (rz+1)*8; z++ {
				if (x-center[0])*(x-center[0])+(z-center[2])*(z-center[2]) <= radius*radius {
					if !yield(celler.ChunkPos{X: int32(x), Z: int32(z)}) {
						return
					}
				}
			}
		}
	}
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
