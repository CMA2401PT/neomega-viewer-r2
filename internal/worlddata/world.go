// Package worlddata 把存档读出的 Java 区块组织为 r2region，并提供近景方块实体。
package worlddata

import (
	"context"
	"maps"
	"slices"

	"celler"
	"celler/java"
	converter "neomega-converter-core"
	"neomega-r2link"
)

// World 将不同来源的 Java 区块变成同一种区域，保持压缩索引和近景方块实体共用状态。
type World struct {
	ReadBlockEntities func(context.Context, celler.ChunkPos) ([]java.BlockEntity, error)
	ReadChunk         func(context.Context, celler.ChunkPos) (*converter.JavaChunk, error)
	ScanChunks        func(context.Context) ([]celler.ChunkPos, error)
}

// visible 判断子区块的高度索引是否落在可显示范围内：Y -64～447。
func visible(y int32) bool { return y >= -4 && y < 28 }

// Chunks 复用原生来源的坐标扫描，地图占用表与区域下载使用同一维度。
func (w *World) Chunks(ctx context.Context) (r2link.ChunkIndex, error) {
	positions, err := w.ScanChunks(ctx)
	if err != nil {
		return nil, err
	}
	index := r2link.ChunkIndex{}
	for _, pos := range positions {
		index.Add(int(pos.X), int(pos.Z))
	}
	return index, nil
}

// Region 遍历这个 r2region 横向覆盖的 8×8 个区块（chunk），收集每个区块已有的子区块（subchunk）。
// 调色板条目换成方块哈希；超出可显示高度的子区块只计数，全空气子区块省略。
func (w *World) Region(ctx context.Context, rx, rz int) (r2link.R2Region, error) {
	var region r2link.R2Region
	for x := rx * 8; x < (rx+1)*8; x++ {
		for z := rz * 8; z < (rz+1)*8; z++ {
			if err := ctx.Err(); err != nil {
				return region, err
			}
			chunk, err := w.ReadChunk(ctx, celler.ChunkPos{X: int32(x), Z: int32(z)})
			if err != nil {
				return region, err
			}
			if chunk == nil {
				continue
			}
			for _, y := range slices.Sorted(maps.Keys(chunk.Sections)) {
				subchunk := chunk.Sections[y]
				if subchunk.Blocks == nil {
					continue
				}
				if !visible(y) {
					region.Clipped++
					continue
				}
				palette := subchunk.Blocks.GetUsedPalette()
				mapped := make([]uint32, len(palette))
				nonAir := false
				for i, state := range palette {
					mapped[i] = r2link.BlockHash(state)
					nonAir = nonAir || !r2link.IsAir(mapped[i])
				}
				if !nonAir {
					continue
				}
				region.Sections = append(region.Sections, r2link.Subchunk{
					Position: [3]int{x, int(y), z},
					Bits:     subchunk.Blocks.Level,
					Palette:  mapped,
					Words:    subchunk.Blocks.Data,
				})
			}
		}
	}
	return region, nil
}
