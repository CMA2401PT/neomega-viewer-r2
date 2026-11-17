package worlddata

import (
	"sync"

	"neomega-blocks/ir"
)

// 整节都是这些状态时不发送；其余状态一律按哈希发送，由渲染器的状态表解释。
var airHashes = map[uint32]bool{
	ir.IRBlock("minecraft:air []").Hash():      true,
	ir.IRBlock("minecraft:cave_air []").Hash(): true,
	ir.IRBlock("minecraft:void_air []").Hash(): true,
}

// AirHash 是空气的哈希，内置场景用它填充空白格。
var AirHash = ir.IRBlock("minecraft:air []").Hash()

// 同一世界反复出现的状态只编码一次；状态集合有限，缓存不会无限增长。
var hashes sync.Map

// Hash 返回方块状态的 neomega-blocks 哈希。
func Hash(state ir.IRBlock) uint32 {
	if value, ok := hashes.Load(state); ok {
		return value.(uint32)
	}
	value := state.Hash()
	hashes.Store(state, value)
	return value
}
