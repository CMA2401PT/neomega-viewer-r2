// fixture 创建独立的只读浏览测试存档；它不属于应用欢迎场景。
package main

import (
	"celler"
	"celler/java"
	"celler/layer_storage"
	"flag"
	"fmt"
	"neomega-blocks/ir"
	nbt "neomega-nbt/presets/v1"
	"os"
)

func main() {
	path := flag.String("out", "", "空目录中的测试存档路径")
	staticModels := flag.Bool("static", false, "添加跨区域的静态模型验证场景")
	flag.Parse()
	if *path == "" {
		panic("需要 -out")
	}
	if err := os.MkdirAll(*path, 0755); err != nil {
		panic(err)
	}
	world, err := java.OpenDirectory(*path, java.Options{})
	if err != nil {
		panic(err)
	}
	defer world.Close()
	storage := layer_storage.MakePaletteStorage(ir.IRBlock("minecraft:air []"))
	for x := 0; x < 16; x++ {
		for z := 0; z < 16; z++ {
			storage.Set(layer_storage.At(x, 0, z), "minecraft:stone []")
			storage.Set(layer_storage.At(x, 1, z), `minecraft:grass_block ["snowy"="false"]`)
		}
	}
	storage.Set(layer_storage.At(8, 2, 8), "minecraft:enchanting_table []")
	storage.Set(layer_storage.At(10, 2, 8), "minecraft:quartz_bricks []")
	chunk := java.NewChunk(celler.ChunkPos{}, 4189)
	if err = chunk.SetSection(&java.Section{Y: 0, Blocks: storage}); err != nil {
		panic(err)
	}
	if err = world.WriteChunk(java.Overworld, chunk); err != nil {
		panic(err)
	}

	if *staticModels {
		// 多个 chunk 共用实际 Java 写入路径，静态模型跨过 x=128 的区域边界。
		blocks := map[[3]int]ir.IRBlock{}
		for x := 118; x <= 138; x++ {
			for z := 120; z <= 138; z++ {
				blocks[[3]int{x, 1, z}] = `minecraft:grass_block ["snowy"="false"]`
			}
		}
		for x := 121; x <= 123; x++ {
			blocks[[3]int{x, 2, 128}] = `minecraft:oak_fence ["east"="true","north"="false","south"="false","west"="true"]`
		}
		blocks[[3]int{126, 2, 128}] = `minecraft:oak_stairs ["facing"="north","half"="bottom","shape"="straight"]`
		blocks[[3]int{130, 2, 128}] = `minecraft:poppy []`
		blocks[[3]int{132, 2, 128}] = `minecraft:dandelion []`
		blocks[[3]int{127, 2, 132}] = `minecraft:oak_sign ["rotation"="0"]`
		blocks[[3]int{134, 2, 128}] = `minecraft:enchanting_table []`
		for cx := 7; cx <= 8; cx++ {
			for cz := 7; cz <= 8; cz++ {
				cells := layer_storage.MakePaletteStorage(ir.IRBlock("minecraft:air []"))
				for p, state := range blocks {
					if p[0]/16 == cx && p[2]/16 == cz {
						cells.Set(layer_storage.At(p[0]%16, p[1], p[2]%16), state)
					}
				}
				chunk := java.NewChunk(celler.ChunkPos{X: int32(cx), Z: int32(cz)}, 4189)
				if err = chunk.SetSection(&java.Section{Y: 0, Blocks: cells}); err != nil {
					panic(err)
				}
				if err = world.WriteChunk(java.Overworld, chunk); err != nil {
					panic(err)
				}
			}
		}
	}
	if err = world.WriteLevel(nbt.Document{Value: nbt.Compound{"Data": nbt.Compound{"SpawnX": int32(8), "SpawnY": int32(4), "SpawnZ": int32(12)}}}); err != nil {
		panic(err)
	}
	if err = world.Commit(); err != nil {
		panic(err)
	}
	fmt.Println(*path)
}
