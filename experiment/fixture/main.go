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
	if err = world.WriteLevel(nbt.Document{Value: nbt.Compound{"Data": nbt.Compound{"SpawnX": int32(8), "SpawnY": int32(4), "SpawnZ": int32(12)}}}); err != nil {
		panic(err)
	}
	if err = world.Commit(); err != nil {
		panic(err)
	}
	fmt.Println(*path)
}
