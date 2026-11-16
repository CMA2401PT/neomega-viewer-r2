package javaworld

import (
	"celler"
	"celler/java"
	"celler/layer_storage"
	"errors"

	"context"
	"fmt"
	"io/fs"
	"neomega-blocks/ir"
	blocks "neomega-blocks/presets/java/v1"
	r2 "neomega-r2"
	"neomega-r2/blockactor"
	"neomega-r2/effector"
	"neomega-r2/object"
	"neomega-viewer-r2/internal/worlddata"
	"os"
	"path/filepath"

	"sync"
)

// World 提供只读 Java 存档的调色板传输；锁仅保护文件句柄，解压与状态映射可并发。
type World struct {
	source  *java.World
	mu      sync.Mutex
	mapping map[string]uint32
}

// StateMapping 在应用启动时共享建立状态映射，区域请求仅转换调色板条目。
// 含水状态目前只发布方块本体，附加水层尚未接入。
func StateMapping(catalog *r2.Catalog) map[string]uint32 {
	mapping := make(map[string]uint32)
	for state := range blocks.JavaCompatibleStates() {
		normalized, ok := blocks.NormalizeSingleLayer(state)
		if !ok {
			continue
		}
		model, ok := catalog.States[string(normalized)]
		if !ok {
			continue
		}
		mapping[string(state)] = model
		for _, value := range []string{"false", "true"} {
			variant := ir.IRBlock(state.NameWithNameSpace() + " " + string(state.States().Set("waterlogged", value))).Tidy()
			mapping[string(variant)] = model
		}
	}
	return mapping
}

// OpenWorld 只读打开 Java 主世界，文件句柄由当前会话独占。
func OpenWorld(path string, mapping map[string]uint32) (*World, error) {
	if filepath.Base(path) == "level.dat" {
		path = filepath.Dir(path)
	}
	if _, err := os.Stat(filepath.Join(path, "level.dat")); err != nil {
		return nil, fmt.Errorf("请选择含 level.dat 的 Java 存档目录：%w", err)
	}
	if _, err := os.Stat(filepath.Join(path, "region")); err != nil {
		return nil, fmt.Errorf("当前入口需要主世界 region 目录：%w", err)
	}
	source, err := java.OpenDirectory(path, java.Options{ReadOnly: true, MaxOpenRegions: 16})
	if err != nil {
		return nil, err
	}
	return &World{source: source, mapping: mapping}, nil
}
func (w *World) Close() error { return w.source.Close() }

// Chunks 只扫描主世界区域文件的占用表，供缩略地图定位存在的区块。
// 每条记录为小端 int32 区域坐标和 128 字节位图，位序按 Z-X 排列、低位在前。
func (w *World) Chunks(ctx context.Context) ([]byte, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	index := make(worlddata.ChunkIndex)
	err := w.source.Scan(java.Overworld, java.Terrain, func(pos celler.ChunkPos) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		index.Add(int(pos.X), int(pos.Z))
		return nil
	})
	if err != nil {
		return nil, err
	}
	return index.Encode(), nil
}

// Region 返回 8×8 区块列的压缩索引，每个调色板条目只携带一个共享静态模型编号。
// 坐标、位宽、调色板长度和压缩字长度均为小端 uint32；坐标按 int32 解释。
func (w *World) Region(ctx context.Context, rx, rz int) ([]byte, error) {
	var sections []worlddata.Section
	var unknown uint32
	for x := rx * 8; x < (rx+1)*8; x++ {
		for z := rz * 8; z < (rz+1)*8; z++ {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			w.mu.Lock()
			record, err := w.source.ReadRaw(java.Overworld, java.Terrain, celler.ChunkPos{X: int32(x), Z: int32(z)})
			w.mu.Unlock()
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			if err != nil {
				return nil, err
			}
			raw, err := record.Decode()
			if err != nil {
				return nil, err
			}
			chunk, err := java.DecodeChunk(raw)
			if err != nil {
				return nil, err
			}
			ys, err := chunk.SectionYs()
			if err != nil {
				return nil, err
			}
			for _, y := range ys {
				if y < -4 || y >= 28 {
					continue
				}
				section, err := chunk.Section(y)
				if err != nil {
					return nil, err
				}
				if section.Blocks == nil {
					continue
				}
				p := section.Blocks
				palette := p.GetUsedPalette()
				mapped := make([]uint32, len(palette))
				nonAir := false
				for i, state := range palette {
					id, ok := w.mapping[string(state)]
					if !ok {
						unknown++
					}
					mapped[i] = id
					nonAir = nonAir || id != 0
				}
				if !nonAir {
					continue
				}
				sections = append(sections, worlddata.Section{
					Position: [3]int{x, int(y), z},
					Bits:     p.Level,
					Palette:  mapped,
					Words:    p.Data,
				})
			}
		}
	}
	return worlddata.EncodeRegion(sections, unknown), nil
}

// Position 使用存档出生点，保持打开不同存档时不依赖实验场景坐标。
func (w *World) Position() ([3]float64, error) {
	level, err := w.source.ReadLevel()
	if err != nil {
		return [3]float64{}, err
	}
	position := [3]float64{0, 80, 0}
	if data, ok := level.Value["Data"].(map[string]any); ok {
		for axis, key := range []string{"SpawnX", "SpawnY", "SpawnZ"} {
			if value, ok := data[key].(int32); ok {
				position[axis] = float64(value)
			}
		}
		position[1] += 12
	}
	return position, nil
}

// Objects 只在近景扫描被注册的方块，解析 NBT 与动画描述不进入远景 Region 路径。
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
			w.mu.Lock()
			record, err := w.source.ReadRaw(java.Overworld, java.Terrain, celler.ChunkPos{X: int32(x), Z: int32(z)})
			w.mu.Unlock()
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			if err != nil {
				return nil, err
			}
			raw, err := record.Decode()
			if err != nil {
				return nil, err
			}
			chunk, err := java.DecodeChunk(raw)
			if err != nil {
				return nil, err
			}
			entities, _, err := chunk.BlockEntities()
			if err != nil {
				return nil, err
			}
			nbts := map[[3]int]blockactor.Input{}
			for _, entry := range entities {
				p := [3]int{int(entry.Pos.X), int(entry.Pos.Y), int(entry.Pos.Z)}
				nbts[p] = blockactor.Input{Position: p, NBT: entry.NBT}
			}
			ys, err := chunk.SectionYs()
			if err != nil {
				return nil, err
			}
			for _, y := range ys {
				if abs(int(y)-center[1]) > vertical {
					continue
				}
				section, err := chunk.Section(y)
				if err != nil {
					return nil, err
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
