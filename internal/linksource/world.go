// Package linksource 保存连接送来的区块快照，并复用文件来源的转换与 R2 区域编码。
package linksource

import (
	"context"
	"fmt"
	"maps"
	"strconv"
	"sync"

	"celler"
	"celler/bedrock"
	"celler/layer_storage"
	"neomega-blocks/ir"
	converter "neomega-converter"
	core "neomega-converter-core"
	"neomega-converter-core/be2je"
	"neomega-r2/blockactor"
	"neomega-r2/effector"
	"neomega-r2/object"
	fixer "neomega-view-utils/fixer/java-postprocess"
	"neomega-viewer-r2/internal/worlddata"
	"neomega-viewer-r2/r2link"
)

var sharedFixer = sync.OnceValues(fixer.New)

// column 分开保存原始和修复结果，邻居计算始终以原始状态为准。
type column struct {
	raw     *core.JavaChunk
	fixed   *core.JavaChunk
	bytes   int
	unknown map[int32]uint64
}

// World 的提交与读取互斥，已返回的区块快照永远不就地改动。
type World struct {
	mu        sync.RWMutex
	adapter   *worlddata.World
	columns   map[celler.ChunkPos]*column
	blocks    string
	dimension int32
	fixer     *fixer.Fixer
	limit     int
	bytes     int
}

// Stats 只汇总当前保留的数据，删除和范围淘汰会同步减少诊断。
type Stats struct {
	Chunks          int    `json:"chunks"`
	Bytes           int    `json:"bytes"`
	ClippedSections int    `json:"clippedSections"`
	UnknownBlocks   uint64 `json:"unknownBlocks"`
}

// New 为一次握手或场景重置创建空表，来源的高度说明保留在服务会话中。
func New(welcome r2link.Message, mapping map[string]uint32, fix bool, limit int) (*World, error) {
	if welcome.Blocks != "java" && welcome.Blocks != "bedrock" {
		return nil, fmt.Errorf("blocks 应为 java 或 bedrock")
	}
	if welcome.Delivery != "push" && welcome.Delivery != "interest" {
		return nil, fmt.Errorf("delivery 应为 push 或 interest")
	}
	if welcome.Scene == nil || welcome.Scene.Height <= 0 {
		return nil, fmt.Errorf("welcome 需要 scene 和正高度")
	}
	world := &World{columns: make(map[celler.ChunkPos]*column), blocks: welcome.Blocks, limit: limit}
	if welcome.Blocks == "bedrock" {
		dimension, err := strconv.ParseInt(welcome.Scene.Dimension, 10, 32)
		if err != nil || dimension < 0 || dimension > 2 {
			return nil, fmt.Errorf("Bedrock dimension 应为 0、1、2")
		}
		world.dimension = int32(dimension)
		if fix {
			world.fixer, err = sharedFixer()
			if err != nil {
				return nil, err
			}
		}
	}
	world.adapter = &worlddata.World{
		Mapping: mapping,
		ReadChunk: func(ctx context.Context, pos celler.ChunkPos) (*core.JavaChunk, error) {
			if column := world.columns[pos]; column != nil {
				return column.fixed, nil
			}
			return nil, nil
		},
		ScanChunks: func(ctx context.Context) ([]celler.ChunkPos, error) {
			positions := make([]celler.ChunkPos, 0, len(world.columns))
			for pos := range world.columns {
				positions = append(positions, pos)
			}
			return positions, nil
		},
	}
	return world, nil
}

// Apply 把一条区块消息及其邻接修复一起提交，超预算时保留上次快照。
func (w *World) Apply(message r2link.Message) ([][2]int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	pos := celler.ChunkPos{X: message.X, Z: message.Z}
	if message.Mode == "delete" {
		return w.commit(map[celler.ChunkPos]*column{pos: nil})
	}
	if message.Mode != "replace" && message.Mode != "patch" {
		return nil, fmt.Errorf("chunk mode 应为 replace、patch 或 delete")
	}
	next := &column{
		raw: &core.JavaChunk{
			Pos: pos, Dimension: w.dimension,
			Sections: map[int32]*core.JavaSection{},
		},
		unknown: map[int32]uint64{},
	}
	if old := w.columns[pos]; message.Mode == "patch" && old != nil {
		next.raw.Sections = maps.Clone(old.raw.Sections)
		next.unknown = maps.Clone(old.unknown)
	}
	for _, y := range message.DeleteSections {
		delete(next.raw.Sections, y)
		delete(next.unknown, y)
	}
	for _, section := range message.Sections {
		count := len(section.Layers)
		if count < 1 || count > 2 || (w.blocks == "java" && count != 1) {
			return nil, fmt.Errorf("Java section 需要一层，Bedrock 需要一或两层")
		}
		delete(next.unknown, section.Y)
		if w.blocks == "java" {
			next.raw.Sections[section.Y] = &core.JavaSection{Blocks: section.Layers[0].Storage()}
		} else {
			source := &bedrock.Section{Y: section.Y, Version: 9}
			for _, layer := range section.Layers {
				storage := layer.Storage()
				source.Layers = append(
					source.Layers,
					&bedrock.Layer{Blocks: layer_storage.ConvertPalette(
						storage,
						func(state ir.IRBlock) bedrock.Block { return bedrock.Block{State: state, Version: 18168865} },
					)},
				)
			}
			converted := be2je.ConvertSection(pos, w.dimension, source, nil)
			next.raw.Sections[section.Y] = converted.Sections[section.Y]
			for _, diagnostic := range converted.Diagnostics {
				if diagnostic.Kind == core.UnknownBlock {
					next.unknown[section.Y] += diagnostic.Count
				}
			}
		}
	}
	return w.commit(map[celler.ChunkPos]*column{pos: next})
}

// Retain 在新的范围生效时清除范围外区块，边界变化也要重算仍保留的邻居。
func (w *World) Retain(ranges []r2link.Range) ([][2]int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	changes := map[celler.ChunkPos]*column{}
	for pos := range w.columns {
		if !r2link.Contains(ranges, pos.X, pos.Z) {
			changes[pos] = nil
		}
	}
	return w.commit(changes)
}

// commit 在锁内构造候选结果，检查预算后才替换共享表。
func (w *World) commit(changes map[celler.ChunkPos]*column) ([][2]int, error) {
	affected := map[celler.ChunkPos]bool{}
	for pos := range changes {
		affected[pos] = true
		if w.fixer != nil {
			for dz := int32(-1); dz <= 1; dz++ {
				for dx := int32(-1); dx <= 1; dx++ {
					neighbour := celler.ChunkPos{X: pos.X + dx, Z: pos.Z + dz}
					if w.columns[neighbour] != nil {
						affected[neighbour] = true
					}
				}
			}
		}
	}
	raw := func(pos celler.ChunkPos) *core.JavaChunk {
		value, ok := changes[pos]
		if !ok {
			value = w.columns[pos]
		}
		if value == nil {
			return nil
		}
		return value.raw
	}
	results := map[celler.ChunkPos]*column{}
	total := w.bytes
	regions := map[[2]int]bool{}
	for pos := range affected {
		if old := w.columns[pos]; old != nil {
			total -= old.bytes
		}
		regions[[2]int{int(pos.X >> 3), int(pos.Z >> 3)}] = true
		center := raw(pos)
		if center == nil {
			results[pos] = nil
			continue
		}
		original, ok := changes[pos]
		if !ok {
			original = w.columns[pos]
		}
		next := *original
		next.fixed = center
		if w.fixer != nil {
			var neighbours [8]*core.JavaChunk
			index := 0
			for dz := int32(-1); dz <= 1; dz++ {
				for dx := int32(-1); dx <= 1; dx++ {
					if dx == 0 && dz == 0 {
						continue
					}
					neighbours[index] = raw(celler.ChunkPos{X: pos.X + dx, Z: pos.Z + dz})
					index++
				}
			}
			next.fixed = converter.FixChunk(w.fixer, center, neighbours)
		}
		next.bytes = columnBytes(&next)
		total += next.bytes
		results[pos] = &next
	}
	if total > w.limit {
		return nil, fmt.Errorf("连接数据超出内存预算（%d / %d 字节）", total, w.limit)
	}
	for pos, value := range results {
		if value == nil {
			delete(w.columns, pos)
		} else {
			w.columns[pos] = value
		}
	}
	w.bytes = total
	keys := make([][2]int, 0, len(regions))
	for key := range regions {
		keys = append(keys, key)
	}
	return keys, nil
}

// columnBytes 按共享调色板去重，修复只改少数 section 时只计一次未改数组。
func columnBytes(value *column) int {
	total := 256 + len(value.unknown)*24
	seen := map[*layer_storage.Palette[ir.IRBlock]]bool{}
	for _, chunk := range []*core.JavaChunk{value.raw, value.fixed} {
		total += len(chunk.Sections) * 32
		for _, section := range chunk.Sections {
			storage := section.Blocks
			if seen[storage] {
				continue
			}
			seen[storage] = true
			total += 128 + len(storage.Data)*4 + len(storage.Palette)*16
			for _, state := range storage.Palette {
				total += len(state)
			}
		}
	}
	return total
}

// Statistics 从当前快照计算说明，断线时仍可查看最后保留内容。
func (w *World) Statistics() Stats {
	w.mu.RLock()
	defer w.mu.RUnlock()
	result := Stats{Chunks: len(w.columns), Bytes: w.bytes}
	for _, column := range w.columns {
		for y := range column.raw.Sections {
			if y < -4 || y >= 28 {
				result.ClippedSections++
			}
		}
		for _, count := range column.unknown {
			result.UnknownBlocks += count
		}
	}
	return result
}

// Region 让一次区域编码看到同一份提交结果。
func (w *World) Region(ctx context.Context, x, z int) ([]byte, error) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.adapter.Region(ctx, x, z)
}

// Chunks 的占用图描述当前保留区块，而非远端全图。
func (w *World) Chunks(ctx context.Context) ([]byte, error) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.adapter.Chunks(ctx)
}

// Objects 与静态区域共享快照，删除方块会同时移除其状态驱动的特效。
func (w *World) Objects(
	ctx context.Context,
	x, z int,
	center [3]int,
	radius int,
	effects effector.Registry,
	actors blockactor.Registry,
) ([]object.Descriptor, error) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.adapter.Objects(
		ctx,
		x,
		z,
		center,
		radius,
		effects,
		actors,
	)
}

// ClippedSections 使用当前保留量，范围淘汰后不会留下历史计数。
func (w *World) ClippedSections() int { return w.Statistics().ClippedSections }

// Close 释放内存表；连接由管理器拥有，场景重置可以继续使用同一传输。
func (w *World) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.columns = map[celler.ChunkPos]*column{}
	w.bytes = 0
	return nil
}
