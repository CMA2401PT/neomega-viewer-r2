package worlddata

import (
	"context"
	"maps"
	"slices"
	"sync"

	"celler"
	converter "neomega-converter-core"
)

// World 将不同来源的 Java 区块变成同一种区域，保持压缩索引和近景对象共用状态。
type World struct {
	HeightDiagnostics bool
	ReadChunk         func(context.Context, celler.ChunkPos) (*converter.JavaChunk, error)
	ScanChunks        func(context.Context) ([]celler.ChunkPos, error)
	mu                sync.Mutex
	clipped           map[celler.ChunkPos]int
}

// chunk 按区块替换诊断计数，使重复下载和近景读取不会重复累计超高 section。
func (w *World) chunk(ctx context.Context, x, z int) (*converter.JavaChunk, error) {
	pos := celler.ChunkPos{X: int32(x), Z: int32(z)}
	chunk, err := w.ReadChunk(ctx, pos)
	if err != nil || chunk == nil {
		return chunk, err
	}
	count := 0
	for y, section := range chunk.Sections {
		if (y < -4 || y >= 28) && section.Blocks != nil {
			count++
		}
	}
	if count > 0 && w.HeightDiagnostics {
		w.mu.Lock()
		if w.clipped == nil {
			w.clipped = make(map[celler.ChunkPos]int)
		}
		w.clipped[pos] = count
		w.mu.Unlock()
	}
	return chunk, nil
}

// ClippedSections 汇总已经读取的区块，避免为了诊断预先解码整个世界。
func (w *World) ClippedSections() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	count := 0
	for _, n := range w.clipped {
		count += n
	}
	return count
}

// Chunks 复用原生来源的坐标扫描，地图占用表与区域下载使用同一维度。
func (w *World) Chunks(ctx context.Context) ([]byte, error) {
	positions, err := w.ScanChunks(ctx)
	if err != nil {
		return nil, err
	}
	index := make(ChunkIndex)
	for _, pos := range positions {
		index.Add(int(pos.X), int(pos.Z))
	}
	return index.Encode(), nil
}

// Region 把调色板条目换成方块哈希，窗口之外的小节计入诊断而不进入固定高度的 GPU 目录。
func (w *World) Region(ctx context.Context, rx, rz int) ([]byte, error) {
	var sections []Section
	for x := rx * 8; x < (rx+1)*8; x++ {
		for z := rz * 8; z < (rz+1)*8; z++ {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			chunk, err := w.chunk(ctx, x, z)
			if err != nil {
				return nil, err
			}
			if chunk == nil {
				continue
			}
			for _, y := range slices.Sorted(maps.Keys(chunk.Sections)) {
				section := chunk.Sections[y]
				if y < -4 || y >= 28 || section.Blocks == nil {
					continue
				}
				palette := section.Blocks.GetUsedPalette()
				mapped := make([]uint32, len(palette))
				nonAir := false
				for i, state := range palette {
					mapped[i] = Hash(state)
					nonAir = nonAir || !airHashes[mapped[i]]
				}
				if !nonAir {
					continue
				}
				sections = append(sections, Section{
					Position: [3]int{x, int(y), z},
					Bits:     section.Blocks.Level,
					Palette:  mapped,
					Words:    section.Blocks.Data,
				})
			}
		}
	}
	return EncodeRegion(sections), nil
}
