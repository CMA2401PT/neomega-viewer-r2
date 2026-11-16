// Package r2link 提供数据源与 R2 共用的消息封装，坐标和状态不依赖渲染资源编号。
package r2link

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math/bits"

	"celler/layer_storage"
	"neomega-blocks/ir"
)

// Scene 声明来源维度的高度，显示窗口由 R2 自己决定。
type Scene struct {
	Name      string `json:"name"`
	Dimension string `json:"dimension"`
	MinY      int    `json:"minY"`
	Height    int    `json:"height"`
}

// Camera 允许只移动位置，未给出的方向保留页面当前朝向。
type Camera struct {
	Position [3]float64 `json:"position"`
	Yaw      *float64   `json:"yaw,omitempty"`
	Pitch    *float64   `json:"pitch,omitempty"`
}

// Range 用包含两端的区块矩形表达供给范围，数据源无需复现渲染窗口算法。
type Range struct {
	MinX int32 `json:"minX"`
	MinZ int32 `json:"minZ"`
	MaxX int32 `json:"maxX"`
	MaxZ int32 `json:"maxZ"`
}

// View 给数据源提供排序所需的相机信息，实际供给以 Ranges 为准。
type View struct {
	Position   [3]float64 `json:"position"`
	Yaw        float64    `json:"yaw"`
	Pitch      float64    `json:"pitch"`
	NearRadius int        `json:"nearRadius"`
	FarRadius  int        `json:"farRadius"`
}

// Layer 的索引放在二进制尾部，第三方只需实现 JSON 和小端位打包。
type Layer struct {
	Palette []string `json:"palette"`
	Bits    uint32   `json:"bits"`
	Words   []uint32 `json:"-"`
}

// Section 每次替换全部层，避免旧水层在主层更新后残留。
type Section struct {
	Y      int32   `json:"y"`
	Layers []Layer `json:"layers"`
}

// Message 共用 TCP 和 WebSocket 的消息体，各类型的必需字段见协议文档。
type Message struct {
	Type           string      `json:"type"`
	Protocol       string      `json:"protocol,omitempty"`
	Version        int         `json:"version,omitempty"`
	Blocks         string      `json:"blocks,omitempty"`
	Delivery       string      `json:"delivery,omitempty"`
	Scene          *Scene      `json:"scene,omitempty"`
	Camera         *Camera     `json:"camera,omitempty"`
	X              int32       `json:"x,omitempty"`
	Z              int32       `json:"z,omitempty"`
	Mode           string      `json:"mode,omitempty"`
	Sections       []Section   `json:"sections,omitempty"`
	DeleteSections []int32     `json:"deleteSections,omitempty"`
	Position       *[3]float64 `json:"position,omitempty"`
	Yaw            *float64    `json:"yaw,omitempty"`
	Pitch          *float64    `json:"pitch,omitempty"`
	Ranges         []Range     `json:"ranges,omitempty"`
	Views          []View      `json:"views,omitempty"`
	Message        string      `json:"message,omitempty"`
}

// Encode 保持头部可读，只把每层压缩字写入尾部。
func Encode(message Message) ([]byte, error) {
	header, err := json.Marshal(message)
	if err != nil {
		return nil, err
	}
	data := binary.LittleEndian.AppendUint32(nil, uint32(len(header)))
	data = append(data, header...)
	for _, section := range message.Sections {
		for _, layer := range section.Layers {
			for _, word := range layer.Words {
				data = binary.LittleEndian.AppendUint32(data, word)
			}
		}
	}
	return data, nil
}

// Decode 检查分帧长度及调色板索引，保证之后的随机读取可以直接使用数组。
func Decode(data []byte) (Message, error) {
	var message Message
	if len(data) < 4 {
		return message, fmt.Errorf("R2Link 消息缺少头长度")
	}
	size := int(binary.LittleEndian.Uint32(data))
	if size > len(data)-4 {
		return message, fmt.Errorf("R2Link JSON 长度超出消息")
	}
	if err := json.Unmarshal(data[4:4+size], &message); err != nil {
		return message, err
	}
	tail := data[4+size:]
	for si := range message.Sections {
		for li := range message.Sections[si].Layers {
			layer := &message.Sections[si].Layers[li]
			n := len(layer.Palette)
			if n < 1 || n > 4096 || layer.Bits != uint32(bits.Len(uint(n-1))) {
				return message, fmt.Errorf("调色板长度与 bits 不匹配")
			}
			words := 0
			if layer.Bits > 0 {
				words = (4096 + int(32/layer.Bits) - 1) / int(32/layer.Bits)
			}
			if len(tail) < words*4 {
				return message, fmt.Errorf("section 索引不完整")
			}
			layer.Words = make([]uint32, words)
			for i := range layer.Words {
				layer.Words[i] = binary.LittleEndian.Uint32(tail[i*4:])
			}
			tail = tail[words*4:]
			if layer.Bits > 0 {
				per := 32 / layer.Bits
				for i := uint32(0); i < 4096; i++ {
					index := layer.Words[i/per] >> (i % per * layer.Bits) & ((1 << layer.Bits) - 1)
					if int(index) >= n {
						return message, fmt.Errorf("调色板索引 %d 超出 %d", index, n)
					}
				}
			}
		}
	}
	if len(tail) != 0 {
		return message, fmt.Errorf("R2Link 消息尾部多出 %d 字节", len(tail))
	}
	return message, nil
}

// Storage 只归一化调色板，索引沿用已经解码的只读快照。
func (l Layer) Storage() *layer_storage.Palette[ir.IRBlock] {
	palette := make([]ir.IRBlock, len(l.Palette))
	for i, state := range l.Palette {
		palette[i] = ir.IRBlock(state).Tidy()
	}
	storage := &layer_storage.Palette[ir.IRBlock]{
		Palette: palette, Data: l.Words, Level: l.Bits,
		LastPaletteIdx: uint32(len(palette) - 1),
	}
	if l.Bits > 0 {
		storage.IdxsPerUint32 = 32 / l.Bits
		storage.Mask = (1 << l.Bits) - 1
	}
	return storage
}

// FromStorage 统一为协议的最小位宽，让内部调色板的容量策略不泄漏给数据源。
func FromStorage(storage *layer_storage.Palette[ir.IRBlock]) Layer {
	palette := storage.GetUsedPalette()
	layer := Layer{Palette: make([]string, len(palette)), Bits: uint32(bits.Len(uint(len(palette) - 1)))}
	for i, state := range palette {
		layer.Palette[i] = string(state)
	}
	if layer.Bits == 0 {
		return layer
	}
	per := 32 / layer.Bits
	layer.Words = make([]uint32, (4096+per-1)/per)
	for i := uint32(0); i < 4096; i++ {
		index := storage.GetIndex(layer_storage.At(i>>8, i&15, i>>4&15))
		layer.Words[i/per] |= uint32(index) << (i % per * layer.Bits)
	}
	return layer
}

// Contains 直接判断矩形并集，负区块坐标与正区块使用同一规则。
func Contains(ranges []Range, x, z int32) bool {
	for _, r := range ranges {
		if x >= r.MinX && x <= r.MaxX && z >= r.MinZ && z <= r.MaxZ {
			return true
		}
	}
	return false
}
