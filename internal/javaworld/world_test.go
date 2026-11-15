package javaworld

import (
	"celler"
	"celler/java"
	"celler/layer_storage"
	"context"
	"encoding/binary"
	"neomega-blocks/ir"
	nbt "neomega-nbt/presets/v1"
	r2 "neomega-r2"
	"testing"
)

// 真实 Java 文件经过传输后，格子顺序与压缩索引保持一致；含水 false 不应导致树叶丢失。
func TestWorldPaletteTransport(t *testing.T) {
	path := t.TempDir()
	source, err := java.OpenDirectory(path, java.Options{})
	if err != nil {
		t.Fatal(err)
	}
	storage := layer_storage.MakePaletteStorage(ir.IRBlock("minecraft:air []"))
	stone := ir.IRBlock("minecraft:stone []")
	leaves := ir.IRBlock(`minecraft:oak_leaves ["distance"="7","persistent"="true","waterlogged"="false"]`)
	storage.Set(layer_storage.At(1, 2, 3), stone)
	storage.Set(layer_storage.At(15, 14, 13), leaves)
	chunk := java.NewChunk(celler.ChunkPos{}, 4189)
	if err = chunk.SetSection(&java.Section{Y: 0, Blocks: storage}); err != nil {
		t.Fatal(err)
	}
	if err = source.WriteChunk(java.Overworld, chunk); err != nil {
		t.Fatal(err)
	}
	if err = source.WriteLevel(nbt.Document{Value: nbt.Compound{"Data": nbt.Compound{}}}); err != nil {
		t.Fatal(err)
	}
	if err = source.Commit(); err != nil {
		t.Fatal(err)
	}
	if err = source.Close(); err != nil {
		t.Fatal(err)
	}
	catalog, err := r2.OpenCatalog()
	if err != nil {
		t.Fatal(err)
	}
	world, err := OpenWorld(path, StateMapping(catalog))
	if err != nil {
		t.Fatal(err)
	}
	defer world.Close()
	data, err := world.Region(context.Background(), 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	read := func(i int) uint32 { return binary.LittleEndian.Uint32(data[i*4:]) }
	if read(0) != 0x32504c54 || read(1) != 1 || read(2) != 0 {
		t.Fatalf("传输头错误：%x %d %d", read(0), read(1), read(2))
	}
	bits, count := read(6), read(7)
	base := 9 + int(count)*2
	for _, sample := range []struct {
		index int
		state ir.IRBlock
	}{{(1 << 8) | (3 << 4) | 2, stone}, {(15 << 8) | (13 << 4) | 14, leaves}, {0, "minecraft:air []"}} {
		per := 32 / int(bits)
		palette := (read(base+sample.index/per) >> uint(sample.index%per*int(bits))) & ((1 << bits) - 1)
		got := read(9 + int(palette)*2)
		want := world.mapping[string(sample.state)][0]
		if got != want {
			t.Fatalf("索引 %d 得到 %d，期望 %d", sample.index, got, want)
		}
		if sample.index != 0 && got == 0 {
			t.Fatalf("方块被丢弃：%s", sample.state)
		}
	}
}
