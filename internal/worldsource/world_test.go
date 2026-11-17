package worldsource

import (
	"celler"
	"celler/java"
	"celler/layer_storage"
	"context"
	"encoding/binary"
	"neomega-blocks/ir"
	"neomega-converter-core/worldinput"
	nbt "neomega-nbt/presets/v1"
	"neomega-r2link"
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
	world, err := Open(context.Background(), Options{Options: worldinput.Options{Path: path}})
	if err != nil {
		t.Fatal(err)
	}
	defer world.Close()
	region, err := world.Region(context.Background(), 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	data := r2link.EncodeRegion(region.Sections)
	read := func(i int) uint32 { return binary.LittleEndian.Uint32(data[i*4:]) }
	if read(0) != r2link.RegionMagic || read(1) != 1 {
		t.Fatalf("传输头错误：%x %d", read(0), read(1))
	}
	bits, count := read(5), read(6)
	base := 8 + int(count)
	for _, sample := range []struct {
		index int
		state ir.IRBlock
	}{{(1 << 8) | (3 << 4) | 2, stone}, {(15 << 8) | (13 << 4) | 14, leaves}, {0, "minecraft:air []"}} {
		per := 32 / int(bits)
		palette := (read(base+sample.index/per) >> uint(sample.index%per*int(bits))) & ((1 << bits) - 1)
		got := read(8 + int(palette))
		want := r2link.BlockHash(sample.state)
		if got != want {
			t.Fatalf("索引 %d 得到 %d，期望 %d", sample.index, got, want)
		}
		if sample.index != 0 && got == r2link.AirHash {
			t.Fatalf("方块被丢弃：%s", sample.state)
		}
	}
}
