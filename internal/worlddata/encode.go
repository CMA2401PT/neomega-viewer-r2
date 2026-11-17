// Package worlddata 让存档与内置场景共用浏览器接收的二进制布局。
package worlddata

import (
	"encoding/binary"
	"slices"
)

// Section 保留压缩索引，避免传输前展开 4096 个格子。
type Section struct {
	Position [3]int
	Bits     uint32
	// Palette 保存方块哈希；渲染器按资源包的状态表换成模型，表外的哈希画成空气。
	Palette []uint32
	Words   []uint32
}

// RegionMagic 标识调色板为方块哈希的区域格式（"TLP4"）。
const RegionMagic = 0x34504c54

// EncodeRegion 把 8×8 区块区域写为统一的小端调色板格式，坐标按 int32 解释。
func EncodeRegion(sections []Section) []byte {
	words := []uint32{RegionMagic, uint32(len(sections))}
	for _, section := range sections {
		words = append(words,
			uint32(section.Position[0]),
			uint32(section.Position[1]),
			uint32(section.Position[2]),
			section.Bits,
			uint32(len(section.Palette)),
			uint32(len(section.Words)),
		)
		words = append(words, section.Palette...)
		words = append(words, section.Words...)
	}
	data := make([]byte, len(words)*4)
	for i, value := range words {
		binary.LittleEndian.PutUint32(data[i*4:], value)
	}
	return data
}

// ChunkIndex 按 32×32 区块聚合占用位，供地图浏览完整来源的分布。
type ChunkIndex map[[2]int][128]byte

// Add 将区块登记为存在；负坐标与 Java 区域文件使用相同的分组方式。
func (index ChunkIndex) Add(x, z int) {
	region := [2]int{x >> 5, z >> 5}
	bitmap := index[region]
	i := (z&31)*32 + (x & 31)
	bitmap[i>>3] |= 1 << (i & 7)
	index[region] = bitmap
}

// Encode 用稳定顺序写出区域坐标和位图，位序按 Z-X 排列、低位在前。
func (index ChunkIndex) Encode() []byte {
	positions := make([][2]int, 0, len(index))
	for position := range index {
		positions = append(positions, position)
	}
	slices.SortFunc(positions, func(a, b [2]int) int {
		if a[0] != b[0] {
			return a[0] - b[0]
		}
		return a[1] - b[1]
	})
	data := make([]byte, len(positions)*136)
	for i, position := range positions {
		offset := i * 136
		binary.LittleEndian.PutUint32(data[offset:], uint32(position[0]))
		binary.LittleEndian.PutUint32(data[offset+4:], uint32(position[1]))
		bitmap := index[position]
		copy(data[offset+8:], bitmap[:])
	}
	return data
}
