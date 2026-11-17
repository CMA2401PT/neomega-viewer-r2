// Package worlddata 把存档读出的 Java 区块变成 R2Link 的区域与近景对象。
package worlddata

import (
	"context"
	"maps"
	"slices"

	"celler"
	converter "neomega-converter-core"
	"neomega-r2link"
)

// World 将不同来源的 Java 区块变成同一种区域，保持压缩索引和近景对象共用状态。
type World struct {
	ReadChunk  func(context.Context, celler.ChunkPos) (*converter.JavaChunk, error)
	ScanChunks func(context.Context) ([]celler.ChunkPos, error)
}

// visible 是可显示的 section 高度：Y -64～447。
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

// Region 把调色板条目换成方块哈希；可显示高度之外的 section 只计数，全是空气的 section 省略。
func (w *World) Region(ctx context.Context, rx, rz int) (r2link.Region, error) {
	var region r2link.Region
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
				section := chunk.Sections[y]
				if section.Blocks == nil {
					continue
				}
				if !visible(y) {
					region.Clipped++
					continue
				}
				palette := section.Blocks.GetUsedPalette()
				mapped := make([]uint32, len(palette))
				nonAir := false
				for i, state := range palette {
					mapped[i] = r2link.BlockHash(state)
					nonAir = nonAir || !r2link.IsAir(mapped[i])
				}
				if !nonAir {
					continue
				}
				region.Sections = append(region.Sections, r2link.Section{
					Position: [3]int{x, int(y), z},
					Bits:     section.Blocks.Level,
					Palette:  mapped,
					Words:    section.Blocks.Data,
				})
			}
		}
	}
	return region, nil
}
