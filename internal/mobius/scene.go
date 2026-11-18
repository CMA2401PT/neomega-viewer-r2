// Package mobius 提供与存档同样可流式读取的海晶灯莫比乌斯环。
package mobius

import (
	"context"
	"math"
	"slices"
	"sync"

	"celler/layer_storage"
	"neomega-r2link"
	"neomega-r2link/blockactor"
	"neomega-r2link/effector"
	"neomega-r2link/object"
)

const (
	SeaLevel   = 64.0
	ringRadius = 46.0
	ringWidth  = 15.0
	ringY      = SeaLevel + ringRadius/2
	twistAngle = math.Pi / 7
)

// Camera 将开场取景与场景一起交给浏览器，避免网页重复维护几何参数。
type Camera struct {
	Position [3]float64 `json:"position"`
	Yaw      float64    `json:"yaw"`
	Pitch    float64    `json:"pitch"`
}

// Scene 只携带内置场景的绘制与开场设置，普通存档不需要这些约定。
type Scene struct {
	Ocean  float64 `json:"ocean"`
	Camera Camera  `json:"camera"`
	Time   float64 `json:"time"`
}

// Opening 用略高于海面的视点容纳环与完整倒影，并略偏向月亮为画面留出边距。
func Opening() *Scene {
	return &Scene{
		Ocean: SeaLevel,
		Camera: Camera{
			Position: [3]float64{-100, 70, 160},
			Yaw:      math.Atan2(100, 160) + 0.08,
			Pitch:    -0.035,
		},
		Time: 19,
	}
}

// World 的只读数据在首次请求时生成，关闭存档后复用同一份区域与占用表。
type World struct {
	data func() sceneData
}

// sceneData 保存按区域分好的 section 与占用表。
type sceneData struct {
	regions map[[2]int]r2link.R2Region
	chunks  r2link.ChunkIndex
}

// New 将几何与编码延迟到首次请求，关闭后仍可复用。
func New() *World {
	return &World{data: sync.OnceValue(func() sceneData {
		sections := make(map[[3]int]*layer_storage.Palette[uint32])
		paintMobiusRing(sections, r2link.BlockHash("minecraft:sea_lantern []"))
		positions := make([][3]int, 0, len(sections))
		for position := range sections {
			positions = append(positions, position)
		}
		slices.SortFunc(positions, func(a, b [3]int) int {
			for i := range a {
				if a[i] != b[i] {
					return a[i] - b[i]
				}
			}
			return 0
		})
		data := sceneData{regions: map[[2]int]r2link.R2Region{}, chunks: r2link.ChunkIndex{}}
		for _, position := range positions {
			blocks := sections[position]
			key := [2]int{position[0] >> 3, position[2] >> 3}
			region := data.regions[key]
			region.Sections = append(region.Sections, r2link.Subchunk{
				Position: position,
				Bits:     blocks.Level,
				Palette:  blocks.GetUsedPalette(),
				Words:    blocks.Data,
			})
			data.regions[key] = region
			data.chunks.Add(position[0], position[2])
		}
		return data
	})}
}

// Region 与存档共用区域格式，浏览器无需识别来源类型。
func (w *World) Region(_ context.Context, x, z int) (r2link.R2Region, error) {
	return w.data().regions[[2]int{x, z}], nil
}

// Chunks 只登记环实际占据的区块，海面由渲染器独立绘制。
func (w *World) Chunks(_ context.Context) (r2link.ChunkIndex, error) {
	return w.data().chunks, nil
}

// Objects 返回空列表，因为海晶灯全部通过静态区域传输。
func (w *World) Objects(
	_ context.Context,
	_, _ int,
	_ [3]int,
	_ int,
	_ effector.Registry,
	_ blockactor.Registry,
) ([]object.Descriptor, error) {
	return []object.Descriptor{}, nil
}

// Close 保留共享场景数据，让之后的默认会话继续借用。
func (w *World) Close() error { return nil }

// paintMobiusRing 沿圆周转半圈，并以小于一格的步长扫描带宽和厚度，体素化后不留缝。
func paintMobiusRing(sections map[[3]int]*layer_storage.Palette[uint32], lantern uint32) {
	const step = 0.45
	steps := int(math.Ceil(2 * math.Pi * (ringRadius + ringWidth) / step))
	phase := math.Pi/2 - twistAngle/2
	for i := range steps {
		angle := 2 * math.Pi * float64(i) / float64(steps)
		twist := angle/2 + phase
		radialX, radialY := math.Cos(angle), math.Sin(angle)
		for v := -ringWidth / 2; v <= ringWidth/2; v += step {
			for t := -0.5; t <= 0.5; t += step {
				radial := ringRadius + v*math.Cos(twist) - t*math.Sin(twist)
				depth := v*math.Sin(twist) + t*math.Cos(twist)
				x := int(math.Floor(radial * radialX))
				y := int(math.Floor(ringY + radial*radialY))
				z := int(math.Floor(depth))
				position := [3]int{x >> 4, y >> 4, z >> 4}
				blocks := sections[position]
				if blocks == nil {
					blocks = layer_storage.MakePaletteStorage(r2link.AirHash)
					sections[position] = blocks
				}
				blocks.Set(layer_storage.At(x&15, y&15, z&15), lantern)
			}
		}
	}
}
